// Package branchsql holds the SQL predicates that every branch read path has to
// agree on. They live in one place because the search fan-out, the MCP branch
// resolvers and the graph edge readers must never disagree about which branches
// are readable (#265).
package branchsql

// DeletedExtractionSourceExclusion drops an extraction branch whose source
// document is gone, so a deleted document's staged objects cannot be read
// through a branch scope (#265, #246). It is written against the kb.branches
// alias b. Two states mean "the source was deleted", and both are reachable
// because deleting a document deletes its extraction jobs but not the branches
// those jobs created:
//
//   - the branch's immutable origin_id names no live job (dangling id), which is
//     the ordinary case: DeleteWithCascade removes the job row, and origin_id has
//     no foreign key to follow it;
//   - the branch carries no origin_id but a job still points at it through
//     staging_branch_id and that job has no live document (an older delete path
//     left the job behind with a null document_id).
//
// A branch with no job link at all is kept: nothing says its source was deleted,
// and origin_id was only introduced in migration 00269 (legacy extraction
// branches were backfilled as manual, not extraction).
const DeletedExtractionSourceExclusion = `NOT (
	b.origin = 'extraction' AND (
		(b.origin_id IS NOT NULL AND NOT EXISTS (
			SELECT 1 FROM kb.object_extraction_jobs j
			WHERE j.id = b.origin_id
			  AND j.document_id IS NOT NULL
			  AND EXISTS (SELECT 1 FROM kb.documents d WHERE d.id = j.document_id)
		))
		OR (b.origin_id IS NULL AND EXISTS (
			SELECT 1 FROM kb.object_extraction_jobs j
			WHERE j.staging_branch_id = b.id
			  AND (j.document_id IS NULL OR NOT EXISTS (
				SELECT 1 FROM kb.documents d WHERE d.id = j.document_id
			))
		))
	)
)`
