package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Per-call caps for a changeset. They mirror CreateSubgraphRequest's object and
// relationship limits so a source that can batch a subgraph can also batch a
// changeset, and an over-limit push is refused rather than truncated.
const (
	changesetMaxObjectItems       = 500
	changesetMaxRelationshipItems = 500
	changesetMaxWithdrawalItems   = 500
)

// Outcomes reported per item. A source reconciles a push from the response
// alone, so "skipped" has to be distinguishable from "not sent".
const (
	ChangesetOutcomeCreated   = "created"
	ChangesetOutcomeUpdated   = "updated"
	ChangesetOutcomeSkipped   = "skipped"
	ChangesetOutcomeWithdrawn = "withdrawn"
	ChangesetOutcomeNotFound  = "not_found"
	ChangesetOutcomeRefused   = "refused"
)

// ChangesetObjectItem is one object upsert in a changeset. The payload fields
// mirror SubgraphObjectRequest; the external fields are the source's own
// identity for the record and are never derived from the graph.
//
// DataClass and OwnerSourceID are deliberately absent: they are derived from the
// changeset's source, not accepted from the client.
type ChangesetObjectItem struct {
	Ref                 string         `json:"_ref,omitempty" validate:"omitempty,max=128"`
	Type                string         `json:"type" validate:"required,max=64"`
	Key                 *string        `json:"key,omitempty" validate:"omitempty,max=128"`
	Status              *string        `json:"status,omitempty" validate:"omitempty,max=64"`
	Properties          map[string]any `json:"properties,omitempty"`
	Labels              []string       `json:"labels,omitempty" validate:"omitempty,max=32,dive,max=64"`
	ExternalID          string         `json:"external_id" validate:"required,max=512"`
	ExternalRevision    *string        `json:"external_revision,omitempty" validate:"omitempty,max=512"`
	ExternalRevisionSeq *int64         `json:"external_revision_seq,omitempty" validate:"omitempty,gte=0"`
}

// ChangesetRelationshipItem is one relationship upsert in a changeset. Endpoints
// follow the subgraph convention: a _ref defined by an object item in the same
// changeset, or an existing UUID.
type ChangesetRelationshipItem struct {
	Type       string         `json:"type" validate:"required,max=64"`
	SrcRef     string         `json:"src_ref,omitempty" validate:"omitempty,max=128"`
	DstRef     string         `json:"dst_ref,omitempty" validate:"omitempty,max=128"`
	SrcID      *uuid.UUID     `json:"src_id,omitempty"`
	DstID      *uuid.UUID     `json:"dst_id,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Weight     *float32       `json:"weight,omitempty"`

	ExternalID          string  `json:"external_id" validate:"required,max=512"`
	ExternalRevision    *string `json:"external_revision,omitempty" validate:"omitempty,max=512"`
	ExternalRevisionSeq *int64  `json:"external_revision_seq,omitempty" validate:"omitempty,gte=0"`
}

// ChangesetWithdrawalItem withdraws one source record. Withdrawal is a state
// change on the contribution: the object stays readable for every other
// contributor.
type ChangesetWithdrawalItem struct {
	SubjectKind string `json:"subject_kind,omitempty" validate:"omitempty,oneof=object relationship"`
	ExternalID  string `json:"external_id" validate:"required,max=512"`
}

// ApplyChangesetRequest is the body of POST /api/graph/changesets.
type ApplyChangesetRequest struct {
	SourceID uuid.UUID `json:"source_id" validate:"required"`
	// BranchID is the branch every item lands on; absent means the main line.
	// One branch for the whole changeset keeps addressing unambiguous: the same
	// external id legitimately exists once per branch.
	BranchID *uuid.UUID `json:"branch_id,omitempty"`
	// Generation is optional. When absent the server allocates the next one for
	// the source; when present it must be greater than the last applied
	// generation, which makes a retried push idempotent at the changeset level.
	Generation    *int64                      `json:"generation,omitempty" validate:"omitempty,gt=0"`
	Objects       []ChangesetObjectItem       `json:"objects,omitempty" validate:"omitempty,max=500,dive"`
	Relationships []ChangesetRelationshipItem `json:"relationships,omitempty" validate:"omitempty,max=500,dive"`
	Withdrawals   []ChangesetWithdrawalItem   `json:"withdrawals,omitempty" validate:"omitempty,max=500,dive"`
}

// ChangesetItemResult reports the fate of one item, in request order.
type ChangesetItemResult struct {
	Kind        string     `json:"kind"`
	ExternalID  string     `json:"external_id"`
	Outcome     string     `json:"outcome"`
	SubjectID   *uuid.UUID `json:"subject_id,omitempty"`
	CanonicalID *uuid.UUID `json:"canonical_id,omitempty"`
	Version     *int       `json:"version,omitempty"`
	Reason      string     `json:"reason,omitempty"`
}

// ApplyChangesetResponse is the result of one apply.
type ApplyChangesetResponse struct {
	GenerationID uuid.UUID             `json:"generation_id"`
	Generation   int64                 `json:"generation"`
	Results      []ChangesetItemResult `json:"results"`
}

// ApplyChangeset applies a source's changeset in a single transaction.
//
// The record is addressed by the source's own external id and revision
// sequence, not by graph ids: a source does not know them. A present
// contribution for (project, branch, source, external_id) is the lookup that
// turns an external record into a graph version; a hit updates that object, a
// miss creates one.
//
// Staleness is decided from the numeric sequence only. The opaque
// external_revision string cannot answer "is this push older than what I have",
// so it is recorded and never compared.
func (s *Service) ApplyChangeset(ctx context.Context, projectID uuid.UUID, req *ApplyChangesetRequest, actorID *uuid.UUID) (*ApplyChangesetResponse, error) {
	if req.SourceID == uuid.Nil {
		return nil, apperror.NewBadRequest("source_id is required")
	}
	if len(req.Objects) > changesetMaxObjectItems {
		return nil, apperror.NewBadRequest(fmt.Sprintf("objects must not exceed %d", changesetMaxObjectItems))
	}
	if len(req.Relationships) > changesetMaxRelationshipItems {
		return nil, apperror.NewBadRequest(fmt.Sprintf("relationships must not exceed %d", changesetMaxRelationshipItems))
	}
	if len(req.Withdrawals) > changesetMaxWithdrawalItems {
		return nil, apperror.NewBadRequest(fmt.Sprintf("withdrawals must not exceed %d", changesetMaxWithdrawalItems))
	}
	if len(req.Objects)+len(req.Relationships)+len(req.Withdrawals) == 0 {
		return nil, apperror.NewBadRequest("changeset is empty")
	}

	// An external id may appear once per changeset. Two items for the same record
	// would race each other inside one transaction, and "the last one wins" is not
	// something a source can reason about.
	if err := checkDuplicateExternalIDs(req.Objects, req.Relationships); err != nil {
		return nil, err
	}

	// Relationship endpoints resolve either inside this changeset (_ref) or to an
	// existing id, exactly like CreateSubgraph; validate before the transaction so
	// a bad reference rejects the whole push with nothing written.
	refSet := make(map[string]bool, len(req.Objects))
	for i, obj := range req.Objects {
		if obj.Ref == "" {
			continue
		}
		if refSet[obj.Ref] {
			return nil, apperror.NewBadRequest(fmt.Sprintf("objects[%d]: duplicate _ref %q", i, obj.Ref))
		}
		refSet[obj.Ref] = true
	}
	for i, rel := range req.Relationships {
		if rel.SrcRef != "" {
			if !refSet[rel.SrcRef] {
				return nil, apperror.NewBadRequest(fmt.Sprintf("relationships[%d]: src_ref %q not found in objects", i, rel.SrcRef))
			}
		} else if rel.SrcID == nil {
			return nil, apperror.NewBadRequest(fmt.Sprintf("relationships[%d]: must specify either src_ref or src_id", i))
		}
		if rel.DstRef != "" {
			if !refSet[rel.DstRef] {
				return nil, apperror.NewBadRequest(fmt.Sprintf("relationships[%d]: dst_ref %q not found in objects", i, rel.DstRef))
			}
		} else if rel.DstID == nil {
			return nil, apperror.NewBadRequest(fmt.Sprintf("relationships[%d]: must specify either src_ref or dst_id", i))
		}
		if rel.SrcRef != "" && rel.SrcRef == rel.DstRef {
			return nil, apperror.NewBadRequest(fmt.Sprintf("relationships[%d]: self-loop not allowed", i))
		}
		if rel.SrcID != nil && rel.DstID != nil && *rel.SrcID == *rel.DstID {
			return nil, apperror.NewBadRequest(fmt.Sprintf("relationships[%d]: self-loop not allowed", i))
		}
	}

	// The source must belong to the caller's project. Ownership *enforcement* is
	// a later slice; this only stops a changeset naming another project's source.
	ok, err := s.repo.SourceExistsInProject(ctx, s.repo.DB(), projectID, req.SourceID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.New(404, "not_found", "source not found in this project")
	}
	if req.BranchID != nil {
		if _, err := s.repo.GetBranchByID(ctx, projectID, *req.BranchID); err != nil {
			if errors.Is(err, apperror.ErrNotFound) {
				return nil, apperror.New(404, "not_found", "branch not found in this project")
			}
			return nil, err
		}
	}

	// Load schemas once, exactly like the subgraph path, so a source push cannot
	// write properties the object's schema rejects.
	var schemas *ExtractionSchemas
	if s.schemaProvider != nil {
		schemas, err = s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
		if err != nil {
			s.log.Warn("failed to load schemas for changeset apply, skipping validation",
				logger.Error(err))
		}
	}
	if err := validateSubgraphObjectKeys(schemas, changesetValidationView(req.Objects)); err != nil {
		return nil, err
	}
	actorType, actorUUID := actorFromContext(ctx, actorID)
	if err := validateSubgraphWorkStatus(actorType, schemas, changesetValidationView(req.Objects)); err != nil {
		return nil, err
	}

	// Bind the caller to the source it names. The request body's source_id is
	// client-supplied, so a project member alone does not establish that the
	// caller *is* that source: the grant row does. The system principal
	// (extraction, ingestion, backfills) is exempt — it is the explicit internal
	// writer, not a credential to be granted. The check runs before BeginTx so a
	// refused push writes no version, contribution or generation row.
	credentialKind, credentialID, _ := auth.CredentialFromContext(ctx)
	if actorType != ActorSystem {
		granted, err := s.repo.HasSourceGrant(ctx, s.repo.DB(), projectID, req.SourceID, credentialKind, credentialID)
		if err != nil {
			return nil, err
		}
		if !granted {
			return nil, apperror.New(409, "owner_conflict",
				"caller is not granted authority to act as this source")
		}
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	results := make([]ChangesetItemResult, 0, len(req.Objects)+len(req.Relationships)+len(req.Withdrawals))
	objByRef := make(map[string]*GraphObject, len(req.Objects))
	objectEmbedIDs := make([]string, 0, len(req.Objects))
	relEmbedIDs := make([]string, 0, len(req.Relationships))

	for i, objItem := range req.Objects {
		res, obj, err := s.applyChangesetObject(ctx, tx.Tx, projectID, req.BranchID, req.SourceID, &objItem, schemas, actorType, actorUUID, credentialKind, credentialID)
		if err != nil {
			return nil, fmt.Errorf("objects[%d] (%s): %w", i, objItem.ExternalID, err)
		}
		results = append(results, res)
		if objItem.Ref != "" && obj != nil {
			objByRef[objItem.Ref] = obj
		}
		if obj != nil && (res.Outcome == ChangesetOutcomeCreated || res.Outcome == ChangesetOutcomeUpdated) {
			objectEmbedIDs = append(objectEmbedIDs, obj.ID.String())
		}
	}

	for i, relItem := range req.Relationships {
		res, relID, err := s.applyChangesetRelationship(ctx, tx.Tx, projectID, req.BranchID, req.SourceID, &relItem, objByRef, schemas, actorType, actorUUID)
		if err != nil {
			return nil, fmt.Errorf("relationships[%d] (%s): %w", i, relItem.ExternalID, err)
		}
		results = append(results, res)
		if relID != nil && (res.Outcome == ChangesetOutcomeCreated || res.Outcome == ChangesetOutcomeUpdated) {
			relEmbedIDs = append(relEmbedIDs, relID.String())
		}
	}

	now := time.Now()
	for i, w := range req.Withdrawals {
		kind := w.SubjectKind
		if kind == "" {
			kind = ContributionSubjectObject
		}
		res, err := s.applyChangesetWithdrawal(ctx, tx.Tx, projectID, req.BranchID, req.SourceID, kind, w.ExternalID, now)
		if err != nil {
			return nil, fmt.Errorf("withdrawals[%d] (%s): %w", i, w.ExternalID, err)
		}
		results = append(results, res)
	}

	generation := int64(0)
	if req.Generation != nil && *req.Generation > 0 {
		var last int64
		if err := tx.NewRaw(
			`SELECT COALESCE(MAX(generation), 0) FROM kb.source_generations WHERE source_id = ?`,
			req.SourceID,
		).Scan(ctx, &last); err != nil {
			return nil, apperror.NewDatabase("Database operation failed", err)
		}
		if *req.Generation <= last {
			return nil, apperror.New(409, "conflict", fmt.Sprintf("generation %d has already been applied for this source (last applied: %d)", *req.Generation, last))
		}
		generation = *req.Generation
	} else {
		generation, err = s.repo.NextSourceGeneration(ctx, tx.Tx, req.SourceID)
		if err != nil {
			return nil, err
		}
	}

	// ItemCount is the number of items that actually changed state, not the
	// number sent: a skipped (stale revision), not_found (missing withdrawal
	// target) or refused (cross-owner without a grant) item is reported in
	// Results but applied nothing, so counting it would overstate the batch.
	applied := 0
	for _, res := range results {
		switch res.Outcome {
		case ChangesetOutcomeCreated, ChangesetOutcomeUpdated, ChangesetOutcomeWithdrawn:
			applied++
		}
	}

	gen := &SourceGeneration{
		ProjectID:  projectID,
		SourceID:   req.SourceID,
		Generation: generation,
		ItemCount:  applied,
	}
	if err := s.repo.InsertSourceGeneration(ctx, tx.Tx, gen); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, apperror.NewDatabase("Database operation failed", err)
	}

	if s.embeddingEnqueuer != nil {
		if len(objectEmbedIDs) > 0 {
			if _, err := s.embeddingEnqueuer.EnqueueBatchEmbeddings(ctx, objectEmbedIDs); err != nil {
				s.log.Warn("failed to enqueue object embeddings after changeset apply", logger.Error(err))
			}
		}
		if len(relEmbedIDs) > 0 {
			if _, err := s.embeddingEnqueuer.EnqueueBatchEmbeddings(ctx, relEmbedIDs); err != nil {
				s.log.Warn("failed to enqueue relationship embeddings after changeset apply", logger.Error(err))
			}
		}
	}

	return &ApplyChangesetResponse{
		GenerationID: gen.ID,
		Generation:   gen.Generation,
		Results:      results,
	}, nil
}

// applyChangesetObject creates or updates the object a source record maps to and
// moves the contribution onto the version it wrote.
func (s *Service) applyChangesetObject(
	ctx context.Context,
	tx bun.Tx,
	projectID uuid.UUID,
	branchID *uuid.UUID,
	sourceID uuid.UUID,
	item *ChangesetObjectItem,
	schemas *ExtractionSchemas,
	actorType string,
	actorID *uuid.UUID,
	credentialKind string,
	credentialID *uuid.UUID,
) (ChangesetItemResult, *GraphObject, error) {
	result := ChangesetItemResult{Kind: ContributionSubjectObject, ExternalID: item.ExternalID}
	seq := revisionSeq(item.ExternalRevisionSeq)

	contrib, err := s.repo.GetPresentContribution(ctx, tx, projectID, sourceID, branchID, ContributionSubjectObject, item.ExternalID)
	if err != nil {
		return result, nil, err
	}

	if contrib != nil {
		if seq <= contrib.ExternalRevisionSeq {
			subjectID := contrib.SubjectVersionID
			result.Outcome = ChangesetOutcomeSkipped
			result.SubjectID = &subjectID
			result.Reason = fmt.Sprintf("stored revision sequence %d is not older than %d", contrib.ExternalRevisionSeq, seq)
			return result, nil, nil
		}

		prevRow, err := s.repo.GetObjectVersionByID(ctx, tx, projectID, contrib.SubjectVersionID)
		if err != nil {
			return result, nil, err
		}
		head, err := s.repo.GetHeadByCanonicalID(ctx, tx, projectID, prevRow.CanonicalID, prevRow.BranchID)
		if err != nil {
			if !errors.Is(err, apperror.ErrNotFound) {
				return result, nil, err
			}
			// The version the contribution arrived on is gone from the head
			// chain (soft-deleted): supersede what is there rather than writing a
			// second head.
			head = prevRow
		}
		adopt := false
		if head.OwnerSourceID != nil && *head.OwnerSourceID != sourceID {
			// The record's head is owned by a different source. Refuse per item
			// unless the caller also holds a grant for that owner, in which case
			// the write adopts it: the caller becomes the owner with
			// data_class 'adopted', and the previous owner stays a contributor.
			// A refusal does not abort the rest of the changeset — the generation
			// row is still written and the other items still apply.
			granted := actorType == ActorSystem
			if !granted {
				var err error
				granted, err = s.repo.HasSourceGrant(ctx, tx, projectID, *head.OwnerSourceID, credentialKind, credentialID)
				if err != nil {
					return result, nil, err
				}
			}
			if !granted {
				result.Outcome = ChangesetOutcomeRefused
				result.Reason = "object is owned by another source and the caller holds no grant for it"
				return result, nil, nil
			}
			adopt = true
		}

		version, err := buildChangesetObjectVersion(head, item, sourceID, schemas, actorType, actorID, adopt, s)
		if err != nil {
			return result, nil, err
		}
		if err := s.repo.CreateVersion(ctx, tx, head, version); err != nil {
			return result, nil, err
		}
		// CreateVersion carried the previous version's contributions onto the
		// new one, so the other sources already follow the object. This
		// source's record still has to leave the version it arrived on — the
		// anchor is not necessarily the head that was just superseded — and the
		// row written below then replaces its inherited copy on the new version.
		if err := s.repo.SupersedeContribution(ctx, tx, contrib.ID); err != nil {
			return result, nil, err
		}
		c := newContribution(projectID, branchID, ContributionSubjectObject, version.ID, sourceID, item.ExternalID, item.ExternalRevision, seq)
		if err := s.repo.InsertContribution(ctx, tx, c); err != nil {
			return result, nil, err
		}

		result.Outcome = ChangesetOutcomeUpdated
		subjectID, canonicalID, v := version.ID, version.CanonicalID, version.Version
		result.SubjectID, result.CanonicalID, result.Version = &subjectID, &canonicalID, &v
		return result, version, nil
	}

	obj, err := buildChangesetObject(projectID, branchID, sourceID, item, schemas, actorType, actorID)
	if err != nil {
		return result, nil, err
	}
	if err := s.repo.CreateInTx(ctx, tx, obj); err != nil {
		return result, nil, err
	}
	c := newContribution(projectID, branchID, ContributionSubjectObject, obj.ID, sourceID, item.ExternalID, item.ExternalRevision, seq)
	if err := s.repo.InsertContribution(ctx, tx, c); err != nil {
		return result, nil, err
	}

	result.Outcome = ChangesetOutcomeCreated
	subjectID, canonicalID, v := obj.ID, obj.CanonicalID, obj.Version
	result.SubjectID, result.CanonicalID, result.Version = &subjectID, &canonicalID, &v
	return result, obj, nil
}

func (s *Service) applyChangesetRelationship(
	ctx context.Context,
	tx bun.Tx,
	projectID uuid.UUID,
	branchID *uuid.UUID,
	sourceID uuid.UUID,
	item *ChangesetRelationshipItem,
	objByRef map[string]*GraphObject,
	schemas *ExtractionSchemas,
	actorType string,
	actorID *uuid.UUID,
) (ChangesetItemResult, *uuid.UUID, error) {
	result := ChangesetItemResult{Kind: ContributionSubjectRelationship, ExternalID: item.ExternalID}
	seq := revisionSeq(item.ExternalRevisionSeq)
	// A relationship type is lower_snake_case by contract: every ordinary write
	// path normalises it (normalizeRelationType) and the search filter lowercases
	// its `type` parameter unconditionally. Storing a pushed type verbatim minted
	// a row that no type-filtered read could find, and let an ordinary create of
	// the same edge add a second row beside it (#140). Object types are not
	// normalised: they are CamelCase by convention (`Person`), not snake_case.
	relType := normalizeRelationType(item.Type)

	contrib, err := s.repo.GetPresentContribution(ctx, tx, projectID, sourceID, branchID, ContributionSubjectRelationship, item.ExternalID)
	if err != nil {
		return result, nil, err
	}
	if contrib != nil && seq <= contrib.ExternalRevisionSeq {
		subjectID := contrib.SubjectVersionID
		result.Outcome = ChangesetOutcomeSkipped
		result.SubjectID = &subjectID
		result.Reason = fmt.Sprintf("stored revision sequence %d is not older than %d", contrib.ExternalRevisionSeq, seq)
		return result, nil, nil
	}

	srcObj, err := resolveChangesetEndpoint(ctx, s, projectID, item.SrcRef, item.SrcID, objByRef, "src")
	if err != nil {
		return result, nil, err
	}
	dstObj, err := resolveChangesetEndpoint(ctx, s, projectID, item.DstRef, item.DstID, objByRef, "dst")
	if err != nil {
		return result, nil, err
	}
	if !branchIDsEqual(srcObj.BranchID, branchID) || !branchIDsEqual(dstObj.BranchID, branchID) {
		return result, nil, apperror.NewBadRequest("relationship endpoints must live on the changeset's branch")
	}
	if !branchIDsEqual(srcObj.BranchID, dstObj.BranchID) {
		return result, nil, apperror.NewBadRequest("relationship endpoints are on different branches")
	}

	if contrib != nil {
		prevRow, err := s.repo.GetRelationshipVersionByID(ctx, tx, projectID, contrib.SubjectVersionID)
		if err != nil {
			return result, nil, err
		}
		head, err := s.repo.GetRelationshipHeadByCanonicalID(ctx, projectID, prevRow.CanonicalID, prevRow.BranchID)
		if err != nil {
			if !errors.Is(err, apperror.ErrNotFound) {
				return result, nil, err
			}
			head = prevRow
		}

		version := *head
		version.ID = uuid.Nil
		version.SupersedesID = nil
		version.ContentHash = nil
		// Same rule as the object path: the caller writing this version owns
		// its stamp and actor, not the superseded row.
		version.CredentialKind = ""
		version.CredentialID = nil
		version.RunID = nil
		version.RootRunID = nil
		version.ActorType = &actorType
		version.ActorID = actorID
		// Set for completeness only: Repository.CreateRelationshipVersion inherits
		// the head's type, because the type is part of the relationship's identity
		// (it is one of the head's unique key columns), so a re-push cannot rename
		// it. Normalising here keeps the invariant true for every assignment we
		// make, rather than relying on the repository to launder it.
		if relType != "" {
			version.Type = relType
		}
		if item.Properties != nil {
			version.Properties = item.Properties
		}
		// Date properties converge on this write path too. The endpoints are
		// known, so the type-specific relationship schema applies. The helper
		// copies first: when the push omits properties, version.Properties
		// aliases head.Properties and must not be mutated in place.
		//
		// Look the schema up by head.Type, not version.Type: the repository
		// inherits the head's type (it is part of the edge's identity), so a
		// pushed type that differs is not the type the row is stored with.
		validated := version.Properties
		if item.Properties != nil {
			var err error
			validated, err = validateChangesetRelationshipProperties(
				version.Properties, schemas, head.Type, srcObj.Type, dstObj.Type)
			if err != nil {
				return result, nil, apperror.NewBadRequest("property validation failed: " + err.Error())
			}
		}
		version.Properties = normaliseChangesetRelationshipDateProperties(
			validated, schemas, head.Type, srcObj.Type, dstObj.Type)
		if item.Weight != nil {
			version.Weight = item.Weight
		}
		version.ChangeSummary = computeChangeSummary(head.Properties, version.Properties)

		if err := s.repo.CreateRelationshipVersion(ctx, tx, head, &version); err != nil {
			return result, nil, err
		}
		// Same as the object path: the relationship version inherited the
		// previous version's contributions, so only this source's own record
		// moves.
		if err := s.repo.SupersedeContribution(ctx, tx, contrib.ID); err != nil {
			return result, nil, err
		}
		c := newContribution(projectID, branchID, ContributionSubjectRelationship, version.ID, sourceID, item.ExternalID, item.ExternalRevision, seq)
		if err := s.repo.InsertContribution(ctx, tx, c); err != nil {
			return result, nil, err
		}

		result.Outcome = ChangesetOutcomeUpdated
		subjectID, canonicalID, v := version.ID, version.CanonicalID, version.Version
		result.SubjectID, result.CanonicalID, result.Version = &subjectID, &canonicalID, &v
		return result, &version.ID, nil
	}

	props := item.Properties
	if props == nil {
		props = map[string]any{}
	}
	validated, err := validateChangesetRelationshipProperties(props, schemas, relType, srcObj.Type, dstObj.Type)
	if err != nil {
		return result, nil, apperror.NewBadRequest("property validation failed: " + err.Error())
	}
	props = normaliseChangesetRelationshipDateProperties(validated, schemas, relType, srcObj.Type, dstObj.Type)
	rel := &GraphRelationship{
		ProjectID:     projectID,
		BranchID:      srcObj.BranchID,
		Type:          relType,
		SrcID:         srcObj.CanonicalID,
		DstID:         dstObj.CanonicalID,
		Properties:    props,
		Weight:        item.Weight,
		Namespace:     srcObj.Namespace,
		ChangeSummary: computeChangeSummary(nil, props),
		ActorType:     &actorType,
		ActorID:       actorID,
	}
	created, err := s.repo.CreateRelationship(ctx, tx, rel)
	if err != nil {
		return result, nil, err
	}
	if !created {
		return result, nil, apperror.New(409, "conflict",
			"a relationship of this type already exists between these objects")
	}
	c := newContribution(projectID, branchID, ContributionSubjectRelationship, rel.ID, sourceID, item.ExternalID, item.ExternalRevision, seq)
	if err := s.repo.InsertContribution(ctx, tx, c); err != nil {
		return result, nil, err
	}

	result.Outcome = ChangesetOutcomeCreated
	subjectID, canonicalID, v := rel.ID, rel.CanonicalID, rel.Version
	result.SubjectID, result.CanonicalID, result.Version = &subjectID, &canonicalID, &v
	return result, &rel.ID, nil
}

func (s *Service) applyChangesetWithdrawal(
	ctx context.Context,
	tx bun.Tx,
	projectID uuid.UUID,
	branchID *uuid.UUID,
	sourceID uuid.UUID,
	kind, externalID string,
	at time.Time,
) (ChangesetItemResult, error) {
	result := ChangesetItemResult{Kind: kind, ExternalID: externalID}

	contrib, err := s.repo.GetPresentContribution(ctx, tx, projectID, sourceID, branchID, kind, externalID)
	if err != nil {
		return result, err
	}
	if contrib == nil {
		result.Outcome = ChangesetOutcomeNotFound
		result.Reason = "no present contribution for this external id"
		return result, nil
	}
	if err := s.repo.WithdrawContribution(ctx, tx, contrib.ID, at); err != nil {
		return result, err
	}

	result.Outcome = ChangesetOutcomeWithdrawn
	subjectID := contrib.SubjectVersionID
	result.SubjectID = &subjectID
	return result, nil
}

// resolveChangesetEndpoint resolves a relationship endpoint by _ref (an object
// in this changeset) or by id (an existing object).
func resolveChangesetEndpoint(ctx context.Context, s *Service, projectID uuid.UUID, ref string, id *uuid.UUID, objByRef map[string]*GraphObject, side string) (*GraphObject, error) {
	if ref != "" {
		obj, ok := objByRef[ref]
		if !ok {
			return nil, apperror.NewBadRequest(fmt.Sprintf("%s_ref %q not found in objects", side, ref))
		}
		return obj, nil
	}
	if id == nil {
		return nil, apperror.NewBadRequest(fmt.Sprintf("must specify either %s_ref or %s_id", side, side))
	}
	obj, err := s.repo.GetByID(ctx, projectID, *id)
	if err != nil {
		return nil, apperror.NewBadRequest(fmt.Sprintf("%s_id %q not found", side, id.String()))
	}
	return obj, nil
}

// buildChangesetObject builds a brand-new source-owned object.
func buildChangesetObject(projectID uuid.UUID, branchID *uuid.UUID, sourceID uuid.UUID, item *ChangesetObjectItem, schemas *ExtractionSchemas, actorType string, actorID *uuid.UUID) (*GraphObject, error) {
	props := item.Properties
	if props == nil {
		props = map[string]any{}
	}

	schemaVersion := ""
	if schema, ok := objectSchema(schemas, item.Type); ok {
		schemaVersion = schema.Version
		coerced, err := checkSchemaProperties(props, schema)
		if err != nil {
			return nil, err
		}
		// Store the coerced map, not the raw one: the changeset/source-push path
		// is a write path and must normalise date properties like every other
		// one (issue #402).
		props = coerced
	}

	obj := &GraphObject{
		ProjectID:     projectID,
		BranchID:      branchID,
		Type:          item.Type,
		Name:          resolveObjectName(nil, props),
		Key:           item.Key,
		Status:        statusForWrite(props, item.Status),
		Properties:    props,
		Labels:        item.Labels,
		ActorType:     &actorType,
		ActorID:       actorID,
		DataClass:     DataClassSource,
		OwnerSourceID: &sourceID,
	}
	if schemaVersion != "" {
		obj.SchemaVersion = &schemaVersion
	}
	return obj, nil
}

// buildChangesetObjectVersion overlays a source push onto the previous head. The
// push is authoritative for the content it owns, but never rewrites the
// `_sources` array: that array belongs to the document-extraction path and this
// capability leaves it alone.
func buildChangesetObjectVersion(head *GraphObject, item *ChangesetObjectItem, sourceID uuid.UUID, schemas *ExtractionSchemas, actorType string, actorID *uuid.UUID, adopt bool, s *Service) (*GraphObject, error) {
	version := *head
	version.ID = uuid.Nil
	version.SupersedesID = nil
	version.ContentHash = nil

	// The new version is written by *this* caller, never by whoever wrote the
	// version it supersedes. Clearing the inherited write stamp and actor lets
	// the insert resolve both from this request's context, exactly as a create
	// does — otherwise version 2 of a record pushed by a second key would still
	// name the first key and its run (#2376).
	version.CredentialKind = ""
	version.CredentialID = nil
	version.RunID = nil
	version.RootRunID = nil
	version.InputHash = nil
	version.ActorType = &actorType
	version.ActorID = actorID

	if item.Type != "" {
		version.Type = item.Type
	}
	if item.Key != nil {
		version.Key = item.Key
	}
	if item.Status != nil {
		version.Status = item.Status
	}
	if item.Labels != nil {
		version.Labels = item.Labels
	}
	if item.Properties != nil {
		props := make(map[string]any, len(item.Properties)+1)
		for k, v := range item.Properties {
			props[k] = v
		}
		if sources, ok := head.Properties[provenanceSourcesKey]; ok {
			props[provenanceSourcesKey] = sources
		}
		version.Properties = props
		if n := resolveObjectName(nil, props); n != nil {
			version.Name = n
		}
	}

	if schema, ok := objectSchema(schemas, version.Type); ok {
		schemaVersion := schema.Version
		version.SchemaVersion = &schemaVersion
		// A source push replaces the property map and may legitimately be
		// partial, so validate the supplied delta without enforcing required
		// fields. Type coercion and enums still apply, exactly as they do on
		// the ordinary patch path. (A brand-new object through
		// buildChangesetObject still runs full validation.) Date values still
		// converge to YYYY-MM-DD (issue #402).
		//
		// Copy first: when the push omits properties, version.Properties still
		// aliases head.Properties, and validating/normalising in place would
		// mutate the previous head the caller is holding.
		version.Properties = copyProperties(version.Properties)
		if item.Properties != nil {
			validated, err := validatePatchProperties(version.Properties, schema)
			if err != nil {
				return nil, apperror.NewBadRequest("property validation failed: " + err.Error())
			}
			version.Properties = validated
		}
		version.Properties = normaliseDateProperties(version.Properties, schema)
	}

	// A source push is authoritative for its own record: the row stays
	// source-classified with the caller as owner. A cross-owner write that holds
	// a grant for the previous owner is an adoption instead — the caller becomes
	// the owner with data_class 'adopted', and the previous owner's contribution
	// is preserved by CreateVersion's inheritance.
	if adopt {
		version.DataClass = DataClassAdopted
	} else {
		version.DataClass = DataClassSource
	}
	version.OwnerSourceID = &sourceID
	return &version, nil
}

func objectSchema(schemas *ExtractionSchemas, objectType string) (agents.ObjectSchema, bool) {
	if schemas == nil || schemas.ObjectSchemas == nil {
		return agents.ObjectSchema{}, false
	}
	schema, ok := schemas.ObjectSchemas[objectType]
	return schema, ok
}

// validateChangesetRelationshipProperties coerces and validates the supplied
// edge-property delta against the matched relationship schema. Required fields
// are not enforced because a source push may legitimately send a partial map.
func validateChangesetRelationshipProperties(props map[string]any, schemas *ExtractionSchemas, relType, srcType, dstType string) (map[string]any, error) {
	if len(props) == 0 || schemas == nil || schemas.RelationshipSchemas == nil || relType == "" {
		return props, nil
	}
	relSchema, ok := relationshipSchemaFor(schemas.RelationshipSchemas, relType, srcType, dstType)
	if !ok || len(relSchema.Properties) == 0 {
		return props, nil
	}
	return validatePatchProperties(props, agents.ObjectSchema{Properties: relSchema.Properties})
}

// normaliseChangesetRelationshipDateProperties normalises an edge's date
// properties on a source push, using the schemas already loaded for the
// changeset. Both endpoint types are known here, so the type-specific
// relationship schema applies (issue #402). The map is copied first: the
// caller's request (or the previous head a version aliases) must not be
// mutated.
func normaliseChangesetRelationshipDateProperties(props map[string]any, schemas *ExtractionSchemas, relType, srcType, dstType string) map[string]any {
	if len(props) == 0 || schemas == nil || schemas.RelationshipSchemas == nil || relType == "" {
		return props
	}
	relSchema, ok := relationshipSchemaFor(schemas.RelationshipSchemas, relType, srcType, dstType)
	if !ok || len(relSchema.Properties) == 0 {
		return props
	}
	objSchema := agents.ObjectSchema{Properties: relSchema.Properties}
	if !schemaHasDateProperties(objSchema) {
		return props
	}
	return normaliseDateProperties(copyProperties(props), objSchema)
}

// checkSchemaProperties validates props and returns the coerced map. Callers
// must store the returned map: `date` coercion is a write-path behaviour, so
// discarding it would let the source-push path store a non-canonical shape.
func checkSchemaProperties(props map[string]any, schema agents.ObjectSchema) (map[string]any, error) {
	coerced, err := validateProperties(props, schema)
	if err != nil {
		return nil, apperror.NewBadRequest("property validation failed: " + err.Error())
	}
	return coerced, nil
}

func newContribution(projectID uuid.UUID, branchID *uuid.UUID, kind string, versionID, sourceID uuid.UUID, externalID string, revision *string, seq int64) *ProvenanceContribution {
	rev := ""
	if revision != nil {
		rev = *revision
	}
	return &ProvenanceContribution{
		ProjectID:           projectID,
		BranchID:            branchID,
		SubjectKind:         kind,
		SubjectVersionID:    versionID,
		SourceID:            sourceID,
		ExternalID:          externalID,
		ExternalRevision:    rev,
		ExternalRevisionSeq: seq,
		State:               ContributionStatePresent,
	}
}

func revisionSeq(seq *int64) int64 {
	if seq == nil {
		return 0
	}
	return *seq
}

// changesetValidationView adapts object items to the shapes the shared subgraph
// guards take, so the board-key and work-status rules apply to a source push
// exactly as they do to a subgraph create.
func changesetValidationView(items []ChangesetObjectItem) []SubgraphObjectRequest {
	out := make([]SubgraphObjectRequest, 0, len(items))
	for _, it := range items {
		out = append(out, SubgraphObjectRequest{
			Ref:        it.Ref,
			Type:       it.Type,
			Key:        it.Key,
			Status:     it.Status,
			Properties: it.Properties,
			Labels:     it.Labels,
			BranchID:   nil,
		})
	}
	return out
}

func checkDuplicateExternalIDs(objects []ChangesetObjectItem, relationships []ChangesetRelationshipItem) error {
	seen := make(map[string]string, len(objects)+len(relationships))
	for i, it := range objects {
		key := ContributionSubjectObject + "/" + it.ExternalID
		if prev, ok := seen[key]; ok {
			return apperror.NewBadRequest(fmt.Sprintf("objects[%d]: duplicate external_id %q (already used by %s)", i, it.ExternalID, prev))
		}
		seen[key] = fmt.Sprintf("objects[%d]", i)
	}
	for i, it := range relationships {
		key := ContributionSubjectRelationship + "/" + it.ExternalID
		if prev, ok := seen[key]; ok {
			return apperror.NewBadRequest(fmt.Sprintf("relationships[%d]: duplicate external_id %q (already used by %s)", i, it.ExternalID, prev))
		}
		seen[key] = fmt.Sprintf("relationships[%d]", i)
	}
	return nil
}

// ObjectContributionsResponse is the read shape of an object version's
// contributions. The HEAD read does not walk history: the version row the
// response names is the row the contributions are keyed to.
type ObjectContributionsResponse struct {
	ObjectID      uuid.UUID                 `json:"object_id"`
	CanonicalID   uuid.UUID                 `json:"canonical_id"`
	Version       int                       `json:"version"`
	Contributions []*ProvenanceContribution `json:"contributions"`
}

// GetObjectContributions returns the contributions recorded on one object
// version. resolveHead follows GET /objects/:id semantics: the current HEAD when
// true, a specific historical version when false.
func (s *Service) GetObjectContributions(ctx context.Context, projectID, id uuid.UUID, resolveHead bool) (*ObjectContributionsResponse, error) {
	var (
		obj *GraphObject
		err error
	)
	if resolveHead {
		obj, err = s.repo.GetByID(ctx, projectID, id)
	} else {
		obj, err = s.repo.GetObjectVersionByID(ctx, s.repo.DB(), projectID, id)
	}
	if err != nil {
		return nil, err
	}

	byVersion, err := s.repo.GetContributions(ctx, projectID, ContributionSubjectObject, []uuid.UUID{obj.ID})
	if err != nil {
		return nil, err
	}
	contributions := byVersion[obj.ID]
	if contributions == nil {
		contributions = []*ProvenanceContribution{}
	}

	return &ObjectContributionsResponse{
		ObjectID:      obj.ID,
		CanonicalID:   obj.CanonicalID,
		Version:       obj.Version,
		Contributions: contributions,
	}, nil
}

// unionMergeContributions carries both sides' present contributions onto the
// version a merge just wrote. The merge's own write stamp stays the merging
// actor's — a contributing source never becomes the writer of the merged row —
// while the contribution rows record which sources back the content and
// merged_from_version_id keeps the version each one arrived on recoverable.
func (s *Service) unionMergeContributions(
	ctx context.Context,
	tx bun.Tx,
	projectID uuid.UUID,
	kind string,
	fromVersionIDs []uuid.UUID,
	toVersionID uuid.UUID,
	toBranchID *uuid.UUID,
) error {
	if _, err := s.repo.CopyContributionsToVersion(ctx, tx, projectID, kind, fromVersionIDs, toVersionID, toBranchID); err != nil {
		return fmt.Errorf("copy %s contributions: %w", kind, err)
	}
	// A merge unions the recorded synthesis inputs of both sides the same way:
	// the merged version was derived from the source document/job as well as the
	// target head, and a reader must be able to say so (#153).
	if _, err := s.repo.copySynthesisLineageToVersion(ctx, tx, kind, fromVersionIDs, toVersionID); err != nil {
		return fmt.Errorf("copy %s synthesis lineage: %w", kind, err)
	}
	return nil
}
