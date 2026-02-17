package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/adk"
)

// =============================================================================
// Schema Complexity Levels
// =============================================================================

// SchemaComplexity defines the complexity level of the extraction schema
type SchemaComplexity string

const (
	SchemaSimple  SchemaComplexity = "simple"  // ~5 entity types, ~5 relationship types
	SchemaMedium  SchemaComplexity = "medium"  // ~10 entity types, ~10 relationship types
	SchemaComplex SchemaComplexity = "complex" // ~15 entity types, ~15 relationship types
)

// SchemaDefinition holds entity and relationship types for a schema
type SchemaDefinition struct {
	Complexity    SchemaComplexity
	EntityTypes   []string
	RelationTypes []string
}

// =============================================================================
// Simple Schema (~5 entity types, ~5 relationship types)
// Focus: Family relationships only
// =============================================================================

var simpleSchema = SchemaDefinition{
	Complexity: SchemaSimple,
	EntityTypes: []string{
		"Person",
		"Place",
		"Event",
		"Organization",
		"Date",
	},
	RelationTypes: []string{
		"PARENT_OF",  // Person -> Person
		"SIBLING_OF", // Person <-> Person
		"MARRIED_TO", // Person <-> Person
		"LIVES_IN",   // Person -> Place
		"WORKS_FOR",  // Person -> Organization
	},
}

// =============================================================================
// Medium Schema (~10 entity types, ~10 relationship types)
// Focus: Extended family, career, education
// =============================================================================

var mediumSchema = SchemaDefinition{
	Complexity: SchemaMedium,
	EntityTypes: []string{
		"Person",
		"Place",
		"Event",
		"Organization",
		"Date",
		"School",
		"Company",
		"Hobby",
		"Pet",
		"Vehicle",
	},
	RelationTypes: []string{
		"PARENT_OF",  // Person -> Person
		"SIBLING_OF", // Person <-> Person
		"MARRIED_TO", // Person <-> Person
		"LIVES_IN",   // Person -> Place
		"WORKS_FOR",  // Person -> Company
		"STUDIED_AT", // Person -> School
		"FRIEND_OF",  // Person <-> Person
		"OWNS",       // Person -> Vehicle/Pet
		"ATTENDED",   // Person -> Event
		"HAS_HOBBY",  // Person -> Hobby
	},
}

// =============================================================================
// Complex Schema (~15 entity types, ~15 relationship types)
// Focus: Full life story with detailed relationships
// =============================================================================

var complexSchema = SchemaDefinition{
	Complexity: SchemaComplex,
	EntityTypes: []string{
		"Person",
		"Place",
		"Event",
		"Organization",
		"Date",
		"School",
		"Company",
		"Hobby",
		"Pet",
		"Vehicle",
		"Project",
		"Award",
		"Publication",
		"Property",
		"Medical_Condition",
	},
	RelationTypes: []string{
		"PARENT_OF",       // Person -> Person
		"SIBLING_OF",      // Person <-> Person
		"MARRIED_TO",      // Person <-> Person
		"LIVES_IN",        // Person -> Place
		"WORKS_FOR",       // Person -> Company
		"STUDIED_AT",      // Person -> School
		"FRIEND_OF",       // Person <-> Person
		"OWNS",            // Person -> Vehicle/Pet/Property
		"ATTENDED",        // Person -> Event
		"HAS_HOBBY",       // Person -> Hobby
		"MENTORED",        // Person -> Person
		"COLLABORATED_ON", // Person -> Project
		"RECEIVED",        // Person -> Award
		"AUTHORED",        // Person -> Publication
		"DIAGNOSED_WITH",  // Person -> Medical_Condition
	},
}

// =============================================================================
// Ground Truth Data - Known entities and relationships for each complexity
// =============================================================================

// GroundTruthEntity represents an entity we expect to extract
type GroundTruthEntity struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

// GroundTruthRelationship represents a relationship we expect to extract
type GroundTruthRelationship struct {
	SourceName  string `json:"source_name"`
	TargetName  string `json:"target_name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// GroundTruth holds all expected entities and relationships
type GroundTruth struct {
	Schema        SchemaDefinition
	Entities      []GroundTruthEntity
	Relationships []GroundTruthRelationship
}

// createSimpleGroundTruth creates ground truth for simple schema
// ~13 entities, ~20 relationships
func createSimpleGroundTruth() *GroundTruth {
	return &GroundTruth{
		Schema: simpleSchema,
		Entities: []GroundTruthEntity{
			// People (8)
			{ID: "person_1", Type: "Person", Name: "Margaret Miller", Description: "Matriarch, age 68"},
			{ID: "person_2", Type: "Person", Name: "Robert Miller", Description: "Patriarch, age 70"},
			{ID: "person_3", Type: "Person", Name: "Sarah Miller", Description: "Eldest daughter, age 42"},
			{ID: "person_4", Type: "Person", Name: "James Miller", Description: "Son, age 38"},
			{ID: "person_5", Type: "Person", Name: "Emily Miller", Description: "Youngest daughter, age 35"},
			{ID: "person_6", Type: "Person", Name: "Michael Chen", Description: "Sarah's husband, age 44"},
			{ID: "person_7", Type: "Person", Name: "Sophie Chen", Description: "Sarah's daughter, age 12"},
			{ID: "person_8", Type: "Person", Name: "Oliver Chen", Description: "Sarah's son, age 9"},

			// Places (3)
			{ID: "place_1", Type: "Place", Name: "Willowbrook", Description: "Family hometown"},
			{ID: "place_2", Type: "Place", Name: "Boston", Description: "Where Sarah lives"},
			{ID: "place_3", Type: "Place", Name: "Chicago", Description: "Robert's birthplace"},

			// Organizations (1)
			{ID: "org_1", Type: "Organization", Name: "Miller Family Foundation", Description: "Family charity"},

			// Events (1)
			{ID: "event_1", Type: "Event", Name: "Miller Family Reunion 2024", Description: "Annual gathering"},
		},
		Relationships: []GroundTruthRelationship{
			// Parent relationships (8)
			{SourceName: "Margaret Miller", TargetName: "Sarah Miller", Type: "PARENT_OF"},
			{SourceName: "Margaret Miller", TargetName: "James Miller", Type: "PARENT_OF"},
			{SourceName: "Margaret Miller", TargetName: "Emily Miller", Type: "PARENT_OF"},
			{SourceName: "Robert Miller", TargetName: "Sarah Miller", Type: "PARENT_OF"},
			{SourceName: "Robert Miller", TargetName: "James Miller", Type: "PARENT_OF"},
			{SourceName: "Robert Miller", TargetName: "Emily Miller", Type: "PARENT_OF"},
			{SourceName: "Sarah Miller", TargetName: "Sophie Chen", Type: "PARENT_OF"},
			{SourceName: "Sarah Miller", TargetName: "Oliver Chen", Type: "PARENT_OF"},

			// Sibling relationships (3)
			{SourceName: "Sarah Miller", TargetName: "James Miller", Type: "SIBLING_OF"},
			{SourceName: "Sarah Miller", TargetName: "Emily Miller", Type: "SIBLING_OF"},
			{SourceName: "Sophie Chen", TargetName: "Oliver Chen", Type: "SIBLING_OF"},

			// Marriage (2)
			{SourceName: "Margaret Miller", TargetName: "Robert Miller", Type: "MARRIED_TO"},
			{SourceName: "Sarah Miller", TargetName: "Michael Chen", Type: "MARRIED_TO"},

			// Location (4)
			{SourceName: "Margaret Miller", TargetName: "Willowbrook", Type: "LIVES_IN"},
			{SourceName: "Robert Miller", TargetName: "Willowbrook", Type: "LIVES_IN"},
			{SourceName: "Sarah Miller", TargetName: "Boston", Type: "LIVES_IN"},
			{SourceName: "Michael Chen", TargetName: "Boston", Type: "LIVES_IN"},

			// Work (1)
			{SourceName: "Robert Miller", TargetName: "Miller Family Foundation", Type: "WORKS_FOR"},
		},
	}
}

// createMediumGroundTruth creates ground truth for medium schema
// ~25 entities, ~40 relationships
func createMediumGroundTruth() *GroundTruth {
	return &GroundTruth{
		Schema: mediumSchema,
		Entities: []GroundTruthEntity{
			// People (13)
			{ID: "person_1", Type: "Person", Name: "Margaret Miller", Description: "Matriarch, retired teacher, age 68"},
			{ID: "person_2", Type: "Person", Name: "Robert Miller", Description: "Patriarch, retired engineer, age 70"},
			{ID: "person_3", Type: "Person", Name: "Sarah Miller", Description: "Doctor, eldest daughter, age 42"},
			{ID: "person_4", Type: "Person", Name: "James Miller", Description: "Software developer, age 38"},
			{ID: "person_5", Type: "Person", Name: "Emily Miller", Description: "Artist, youngest daughter, age 35"},
			{ID: "person_6", Type: "Person", Name: "Michael Chen", Description: "Architect, Sarah's husband, age 44"},
			{ID: "person_7", Type: "Person", Name: "Lisa Park", Description: "Professor, James's wife, age 37"},
			{ID: "person_8", Type: "Person", Name: "David Thompson", Description: "Musician, Emily's friend, age 36"},
			{ID: "person_9", Type: "Person", Name: "Anna Schmidt", Description: "Margaret's childhood friend, age 67"},
			{ID: "person_10", Type: "Person", Name: "Sophie Chen", Description: "Sarah's daughter, age 12"},
			{ID: "person_11", Type: "Person", Name: "Oliver Chen", Description: "Sarah's son, age 9"},
			{ID: "person_12", Type: "Person", Name: "Emma Park-Miller", Description: "James's daughter, age 5"},
			{ID: "person_13", Type: "Person", Name: "Thomas Wright", Description: "Robert's colleague, age 69"},

			// Places (5)
			{ID: "place_1", Type: "Place", Name: "Willowbrook", Description: "Family hometown in Vermont"},
			{ID: "place_2", Type: "Place", Name: "Boston", Description: "Where Sarah and Michael live"},
			{ID: "place_3", Type: "Place", Name: "San Francisco", Description: "Where James and Lisa live"},
			{ID: "place_4", Type: "Place", Name: "Portland", Description: "Where Emily has her studio"},
			{ID: "place_5", Type: "Place", Name: "Vienna", Description: "City Margaret visited"},

			// Schools (2)
			{ID: "school_1", Type: "School", Name: "Harvard Medical School", Description: "Where Sarah studied"},
			{ID: "school_2", Type: "School", Name: "Stanford University", Description: "Where James studied"},

			// Companies (2)
			{ID: "company_1", Type: "Company", Name: "Boston General Hospital", Description: "Where Sarah works"},
			{ID: "company_2", Type: "Company", Name: "TechCorp", Description: "Where James works"},

			// Hobbies (2)
			{ID: "hobby_1", Type: "Hobby", Name: "Gardening", Description: "Margaret's passion"},
			{ID: "hobby_2", Type: "Hobby", Name: "Golf", Description: "Robert's pastime"},

			// Pets (1)
			{ID: "pet_1", Type: "Pet", Name: "Max", Description: "Family golden retriever"},

			// Events (1)
			{ID: "event_1", Type: "Event", Name: "Miller Family Reunion 2024", Description: "Annual gathering in Willowbrook"},

			// Vehicles (1)
			{ID: "vehicle_1", Type: "Vehicle", Name: "1967 Mustang", Description: "Robert's classic car"},
		},
		Relationships: []GroundTruthRelationship{
			// Parent relationships (10)
			{SourceName: "Margaret Miller", TargetName: "Sarah Miller", Type: "PARENT_OF"},
			{SourceName: "Margaret Miller", TargetName: "James Miller", Type: "PARENT_OF"},
			{SourceName: "Margaret Miller", TargetName: "Emily Miller", Type: "PARENT_OF"},
			{SourceName: "Robert Miller", TargetName: "Sarah Miller", Type: "PARENT_OF"},
			{SourceName: "Robert Miller", TargetName: "James Miller", Type: "PARENT_OF"},
			{SourceName: "Robert Miller", TargetName: "Emily Miller", Type: "PARENT_OF"},
			{SourceName: "Sarah Miller", TargetName: "Sophie Chen", Type: "PARENT_OF"},
			{SourceName: "Sarah Miller", TargetName: "Oliver Chen", Type: "PARENT_OF"},
			{SourceName: "Michael Chen", TargetName: "Sophie Chen", Type: "PARENT_OF"},
			{SourceName: "James Miller", TargetName: "Emma Park-Miller", Type: "PARENT_OF"},

			// Sibling relationships (4)
			{SourceName: "Sarah Miller", TargetName: "James Miller", Type: "SIBLING_OF"},
			{SourceName: "Sarah Miller", TargetName: "Emily Miller", Type: "SIBLING_OF"},
			{SourceName: "James Miller", TargetName: "Emily Miller", Type: "SIBLING_OF"},
			{SourceName: "Sophie Chen", TargetName: "Oliver Chen", Type: "SIBLING_OF"},

			// Marriage (3)
			{SourceName: "Margaret Miller", TargetName: "Robert Miller", Type: "MARRIED_TO"},
			{SourceName: "Sarah Miller", TargetName: "Michael Chen", Type: "MARRIED_TO"},
			{SourceName: "James Miller", TargetName: "Lisa Park", Type: "MARRIED_TO"},

			// Friend relationships (3)
			{SourceName: "Emily Miller", TargetName: "David Thompson", Type: "FRIEND_OF"},
			{SourceName: "Margaret Miller", TargetName: "Anna Schmidt", Type: "FRIEND_OF"},
			{SourceName: "Robert Miller", TargetName: "Thomas Wright", Type: "FRIEND_OF"},

			// Location (6)
			{SourceName: "Margaret Miller", TargetName: "Willowbrook", Type: "LIVES_IN"},
			{SourceName: "Robert Miller", TargetName: "Willowbrook", Type: "LIVES_IN"},
			{SourceName: "Sarah Miller", TargetName: "Boston", Type: "LIVES_IN"},
			{SourceName: "James Miller", TargetName: "San Francisco", Type: "LIVES_IN"},
			{SourceName: "Emily Miller", TargetName: "Portland", Type: "LIVES_IN"},
			{SourceName: "Lisa Park", TargetName: "San Francisco", Type: "LIVES_IN"},

			// Education (2)
			{SourceName: "Sarah Miller", TargetName: "Harvard Medical School", Type: "STUDIED_AT"},
			{SourceName: "James Miller", TargetName: "Stanford University", Type: "STUDIED_AT"},

			// Work (2)
			{SourceName: "Sarah Miller", TargetName: "Boston General Hospital", Type: "WORKS_FOR"},
			{SourceName: "James Miller", TargetName: "TechCorp", Type: "WORKS_FOR"},

			// Hobbies (2)
			{SourceName: "Margaret Miller", TargetName: "Gardening", Type: "HAS_HOBBY"},
			{SourceName: "Robert Miller", TargetName: "Golf", Type: "HAS_HOBBY"},

			// Ownership (2)
			{SourceName: "Robert Miller", TargetName: "Max", Type: "OWNS"},
			{SourceName: "Robert Miller", TargetName: "1967 Mustang", Type: "OWNS"},

			// Event attendance (3)
			{SourceName: "Margaret Miller", TargetName: "Miller Family Reunion 2024", Type: "ATTENDED"},
			{SourceName: "Sarah Miller", TargetName: "Miller Family Reunion 2024", Type: "ATTENDED"},
			{SourceName: "Emily Miller", TargetName: "Miller Family Reunion 2024", Type: "ATTENDED"},
		},
	}
}

// createComplexGroundTruth creates ground truth for complex schema
// ~40 entities, ~60+ relationships
func createComplexGroundTruth() *GroundTruth {
	return &GroundTruth{
		Schema: complexSchema,
		Entities: []GroundTruthEntity{
			// People (15)
			{ID: "person_1", Type: "Person", Name: "Margaret Miller", Description: "Matriarch, retired teacher, age 68, cancer survivor"},
			{ID: "person_2", Type: "Person", Name: "Robert Miller", Description: "Patriarch, retired engineer, age 70, invented water purification system"},
			{ID: "person_3", Type: "Person", Name: "Sarah Miller", Description: "Renowned cardiologist, eldest daughter, age 42"},
			{ID: "person_4", Type: "Person", Name: "James Miller", Description: "Tech entrepreneur, age 38, founded startup"},
			{ID: "person_5", Type: "Person", Name: "Emily Miller", Description: "Award-winning artist, youngest daughter, age 35"},
			{ID: "person_6", Type: "Person", Name: "Michael Chen", Description: "Architect, designed sustainable buildings, age 44"},
			{ID: "person_7", Type: "Person", Name: "Lisa Park", Description: "Professor of literature, published author, age 37"},
			{ID: "person_8", Type: "Person", Name: "David Thompson", Description: "Jazz musician, Grammy nominee, age 36"},
			{ID: "person_9", Type: "Person", Name: "Anna Schmidt", Description: "Margaret's childhood friend, retired nurse, age 67"},
			{ID: "person_10", Type: "Person", Name: "Thomas Wright", Description: "Robert's colleague, inventor, age 69"},
			{ID: "person_11", Type: "Person", Name: "Sophie Chen", Description: "Sarah's daughter, aspiring scientist, age 12"},
			{ID: "person_12", Type: "Person", Name: "Oliver Chen", Description: "Sarah's son, musician prodigy, age 9"},
			{ID: "person_13", Type: "Person", Name: "Emma Park-Miller", Description: "James's daughter, age 5"},
			{ID: "person_14", Type: "Person", Name: "Dr. Helen Foster", Description: "Sarah's mentor at Harvard, age 65"},
			{ID: "person_15", Type: "Person", Name: "Marcus Johnson", Description: "James's business partner, age 40"},

			// Places (6)
			{ID: "place_1", Type: "Place", Name: "Willowbrook", Description: "Family hometown in Vermont, population 5000"},
			{ID: "place_2", Type: "Place", Name: "Boston", Description: "Where Sarah practices medicine"},
			{ID: "place_3", Type: "Place", Name: "San Francisco", Description: "Tech hub where James's company is based"},
			{ID: "place_4", Type: "Place", Name: "Portland", Description: "Arts district where Emily has her gallery"},
			{ID: "place_5", Type: "Place", Name: "Vienna", Description: "Where Margaret attended music festival"},
			{ID: "place_6", Type: "Place", Name: "Tokyo", Description: "Where James presented at tech conference"},

			// Schools (3)
			{ID: "school_1", Type: "School", Name: "Harvard Medical School", Description: "Sarah's alma mater"},
			{ID: "school_2", Type: "School", Name: "Stanford University", Description: "James's alma mater"},
			{ID: "school_3", Type: "School", Name: "Rhode Island School of Design", Description: "Emily's art school"},

			// Companies (3)
			{ID: "company_1", Type: "Company", Name: "Boston General Hospital", Description: "Leading medical institution"},
			{ID: "company_2", Type: "Company", Name: "InnovateTech Solutions", Description: "James's startup company"},
			{ID: "company_3", Type: "Company", Name: "Chen Architecture Firm", Description: "Michael's design firm"},

			// Hobbies (3)
			{ID: "hobby_1", Type: "Hobby", Name: "Gardening", Description: "Margaret's therapeutic passion"},
			{ID: "hobby_2", Type: "Hobby", Name: "Golf", Description: "Robert's retirement activity"},
			{ID: "hobby_3", Type: "Hobby", Name: "Photography", Description: "Emily's secondary art form"},

			// Pets (2)
			{ID: "pet_1", Type: "Pet", Name: "Max", Description: "Family golden retriever, age 7"},
			{ID: "pet_2", Type: "Pet", Name: "Luna", Description: "Emily's rescue cat"},

			// Vehicles (2)
			{ID: "vehicle_1", Type: "Vehicle", Name: "1967 Mustang", Description: "Robert's restored classic"},
			{ID: "vehicle_2", Type: "Vehicle", Name: "Tesla Model S", Description: "James's electric car"},

			// Projects (2)
			{ID: "project_1", Type: "Project", Name: "Clean Water Initiative", Description: "Robert's philanthropic project"},
			{ID: "project_2", Type: "Project", Name: "Cardio AI System", Description: "Sarah's research project"},

			// Awards (3)
			{ID: "award_1", Type: "Award", Name: "National Art Prize 2022", Description: "Emily's recognition"},
			{ID: "award_2", Type: "Award", Name: "Medical Innovation Award", Description: "Sarah's research recognition"},
			{ID: "award_3", Type: "Award", Name: "Engineering Excellence Medal", Description: "Robert's career achievement"},

			// Publications (2)
			{ID: "pub_1", Type: "Publication", Name: "Modern Cardiac Care", Description: "Sarah's medical textbook"},
			{ID: "pub_2", Type: "Publication", Name: "Poetry of the Midwest", Description: "Lisa's anthology"},

			// Properties (2)
			{ID: "prop_1", Type: "Property", Name: "Miller Homestead", Description: "Family house in Willowbrook since 1955"},
			{ID: "prop_2", Type: "Property", Name: "Beach Cottage", Description: "Vacation home in Cape Cod"},

			// Medical Conditions (1)
			{ID: "condition_1", Type: "Medical_Condition", Name: "Breast Cancer", Description: "Margaret's 2018 diagnosis, now in remission"},

			// Events (2)
			{ID: "event_1", Type: "Event", Name: "Miller Family Reunion 2024", Description: "75th anniversary gathering"},
			{ID: "event_2", Type: "Event", Name: "Emily's Gallery Opening 2023", Description: "Portland art exhibition"},
		},
		Relationships: []GroundTruthRelationship{
			// Parent relationships (12)
			{SourceName: "Margaret Miller", TargetName: "Sarah Miller", Type: "PARENT_OF"},
			{SourceName: "Margaret Miller", TargetName: "James Miller", Type: "PARENT_OF"},
			{SourceName: "Margaret Miller", TargetName: "Emily Miller", Type: "PARENT_OF"},
			{SourceName: "Robert Miller", TargetName: "Sarah Miller", Type: "PARENT_OF"},
			{SourceName: "Robert Miller", TargetName: "James Miller", Type: "PARENT_OF"},
			{SourceName: "Robert Miller", TargetName: "Emily Miller", Type: "PARENT_OF"},
			{SourceName: "Sarah Miller", TargetName: "Sophie Chen", Type: "PARENT_OF"},
			{SourceName: "Sarah Miller", TargetName: "Oliver Chen", Type: "PARENT_OF"},
			{SourceName: "Michael Chen", TargetName: "Sophie Chen", Type: "PARENT_OF"},
			{SourceName: "Michael Chen", TargetName: "Oliver Chen", Type: "PARENT_OF"},
			{SourceName: "James Miller", TargetName: "Emma Park-Miller", Type: "PARENT_OF"},
			{SourceName: "Lisa Park", TargetName: "Emma Park-Miller", Type: "PARENT_OF"},

			// Sibling relationships (4)
			{SourceName: "Sarah Miller", TargetName: "James Miller", Type: "SIBLING_OF"},
			{SourceName: "Sarah Miller", TargetName: "Emily Miller", Type: "SIBLING_OF"},
			{SourceName: "James Miller", TargetName: "Emily Miller", Type: "SIBLING_OF"},
			{SourceName: "Sophie Chen", TargetName: "Oliver Chen", Type: "SIBLING_OF"},

			// Marriage (3)
			{SourceName: "Margaret Miller", TargetName: "Robert Miller", Type: "MARRIED_TO"},
			{SourceName: "Sarah Miller", TargetName: "Michael Chen", Type: "MARRIED_TO"},
			{SourceName: "James Miller", TargetName: "Lisa Park", Type: "MARRIED_TO"},

			// Friend relationships (4)
			{SourceName: "Emily Miller", TargetName: "David Thompson", Type: "FRIEND_OF"},
			{SourceName: "Margaret Miller", TargetName: "Anna Schmidt", Type: "FRIEND_OF"},
			{SourceName: "Robert Miller", TargetName: "Thomas Wright", Type: "FRIEND_OF"},
			{SourceName: "James Miller", TargetName: "Marcus Johnson", Type: "FRIEND_OF"},

			// Location (8)
			{SourceName: "Margaret Miller", TargetName: "Willowbrook", Type: "LIVES_IN"},
			{SourceName: "Robert Miller", TargetName: "Willowbrook", Type: "LIVES_IN"},
			{SourceName: "Sarah Miller", TargetName: "Boston", Type: "LIVES_IN"},
			{SourceName: "Michael Chen", TargetName: "Boston", Type: "LIVES_IN"},
			{SourceName: "James Miller", TargetName: "San Francisco", Type: "LIVES_IN"},
			{SourceName: "Lisa Park", TargetName: "San Francisco", Type: "LIVES_IN"},
			{SourceName: "Emily Miller", TargetName: "Portland", Type: "LIVES_IN"},
			{SourceName: "David Thompson", TargetName: "Portland", Type: "LIVES_IN"},

			// Education (3)
			{SourceName: "Sarah Miller", TargetName: "Harvard Medical School", Type: "STUDIED_AT"},
			{SourceName: "James Miller", TargetName: "Stanford University", Type: "STUDIED_AT"},
			{SourceName: "Emily Miller", TargetName: "Rhode Island School of Design", Type: "STUDIED_AT"},

			// Work (4)
			{SourceName: "Sarah Miller", TargetName: "Boston General Hospital", Type: "WORKS_FOR"},
			{SourceName: "James Miller", TargetName: "InnovateTech Solutions", Type: "WORKS_FOR"},
			{SourceName: "Michael Chen", TargetName: "Chen Architecture Firm", Type: "WORKS_FOR"},
			{SourceName: "Marcus Johnson", TargetName: "InnovateTech Solutions", Type: "WORKS_FOR"},

			// Hobbies (3)
			{SourceName: "Margaret Miller", TargetName: "Gardening", Type: "HAS_HOBBY"},
			{SourceName: "Robert Miller", TargetName: "Golf", Type: "HAS_HOBBY"},
			{SourceName: "Emily Miller", TargetName: "Photography", Type: "HAS_HOBBY"},

			// Ownership (6)
			{SourceName: "Robert Miller", TargetName: "Max", Type: "OWNS"},
			{SourceName: "Emily Miller", TargetName: "Luna", Type: "OWNS"},
			{SourceName: "Robert Miller", TargetName: "1967 Mustang", Type: "OWNS"},
			{SourceName: "James Miller", TargetName: "Tesla Model S", Type: "OWNS"},
			{SourceName: "Robert Miller", TargetName: "Miller Homestead", Type: "OWNS"},
			{SourceName: "Margaret Miller", TargetName: "Beach Cottage", Type: "OWNS"},

			// Mentorship (2)
			{SourceName: "Dr. Helen Foster", TargetName: "Sarah Miller", Type: "MENTORED"},
			{SourceName: "Robert Miller", TargetName: "James Miller", Type: "MENTORED"},

			// Project collaboration (3)
			{SourceName: "Robert Miller", TargetName: "Clean Water Initiative", Type: "COLLABORATED_ON"},
			{SourceName: "Sarah Miller", TargetName: "Cardio AI System", Type: "COLLABORATED_ON"},
			{SourceName: "Thomas Wright", TargetName: "Clean Water Initiative", Type: "COLLABORATED_ON"},

			// Awards (3)
			{SourceName: "Emily Miller", TargetName: "National Art Prize 2022", Type: "RECEIVED"},
			{SourceName: "Sarah Miller", TargetName: "Medical Innovation Award", Type: "RECEIVED"},
			{SourceName: "Robert Miller", TargetName: "Engineering Excellence Medal", Type: "RECEIVED"},

			// Publications (2)
			{SourceName: "Sarah Miller", TargetName: "Modern Cardiac Care", Type: "AUTHORED"},
			{SourceName: "Lisa Park", TargetName: "Poetry of the Midwest", Type: "AUTHORED"},

			// Medical condition (1)
			{SourceName: "Margaret Miller", TargetName: "Breast Cancer", Type: "DIAGNOSED_WITH"},

			// Event attendance (4)
			{SourceName: "Margaret Miller", TargetName: "Miller Family Reunion 2024", Type: "ATTENDED"},
			{SourceName: "Robert Miller", TargetName: "Miller Family Reunion 2024", Type: "ATTENDED"},
			{SourceName: "Sarah Miller", TargetName: "Emily's Gallery Opening 2023", Type: "ATTENDED"},
			{SourceName: "David Thompson", TargetName: "Emily's Gallery Opening 2023", Type: "ATTENDED"},
		},
	}
}

// =============================================================================
// Story Generation
// =============================================================================

// buildStoryGenerationPrompt creates a prompt to generate a story with our entities
func buildStoryGenerationPrompt(groundTruth *GroundTruth) string {
	// Build entity list by type
	entityByType := make(map[string][]GroundTruthEntity)
	for _, e := range groundTruth.Entities {
		entityByType[e.Type] = append(entityByType[e.Type], e)
	}

	var entityList strings.Builder
	for _, entityType := range groundTruth.Schema.EntityTypes {
		if entities, ok := entityByType[entityType]; ok && len(entities) > 0 {
			entityList.WriteString(fmt.Sprintf("\n## %ss\n", entityType))
			for _, e := range entities {
				entityList.WriteString(fmt.Sprintf("- **%s**: %s\n", e.Name, e.Description))
			}
		}
	}

	// Build relationship list by type
	relByType := make(map[string][]GroundTruthRelationship)
	for _, r := range groundTruth.Relationships {
		relByType[r.Type] = append(relByType[r.Type], r)
	}

	var relList strings.Builder
	relList.WriteString("\n## Key Relationships to Include\n")
	for _, relType := range groundTruth.Schema.RelationTypes {
		if rels, ok := relByType[relType]; ok && len(rels) > 0 {
			relList.WriteString(fmt.Sprintf("\n### %s\n", relType))
			for _, r := range rels {
				relList.WriteString(fmt.Sprintf("- %s → %s\n", r.SourceName, r.TargetName))
			}
		}
	}

	wordCount := 15000
	if groundTruth.Schema.Complexity == SchemaSimple {
		wordCount = 10000
	} else if groundTruth.Schema.Complexity == SchemaComplex {
		wordCount = 20000
	}

	return fmt.Sprintf(`You are a creative fiction writer. Write a detailed family saga story (approximately %d words, about %d pages) that naturally incorporates ALL of the following characters, places, and relationships.

# Characters and Entities
%s

# Relationships
%s

## Writing Requirements

1. **Story Structure**: Write a multi-chapter narrative following the Miller family
2. **Character Development**: Give each character distinct personality traits
3. **Relationship Depth**: Show dynamics between characters - conversations, conflicts, reconciliations
4. **Natural Integration**: Weave all relationships naturally into the story
5. **Entity Mentions**: Mention EVERY entity by name at least 2-3 times
6. **Relationship Clarity**: Explicitly state relationships (e.g., "her mother Margaret", "his company InnovateTech")

## Important
- Include ALL entities listed above
- Explicitly depict ALL relationships
- Use full names when introducing characters

Write the complete story now. Begin with "Chapter 1".`, wordCount, wordCount/3000, entityList.String(), relList.String())
}

// generateStory uses the LLM to generate a story based on our ground truth
func generateStory(ctx context.Context, llm adkmodel.LLM, groundTruth *GroundTruth) (string, error) {
	prompt := buildStoryGenerationPrompt(groundTruth)

	config := &genai.GenerateContentConfig{
		Temperature:     ptr(float32(0.7)),
		MaxOutputTokens: 8192, // Max allowed by model
	}

	llmRequest := &adkmodel.LLMRequest{
		Contents: []*genai.Content{
			{
				Role:  "user",
				Parts: []*genai.Part{{Text: prompt}},
			},
		},
		Config: config,
	}

	var story strings.Builder
	var lastErr error

	for resp, err := range llm.GenerateContent(ctx, llmRequest, false) {
		if err != nil {
			lastErr = err
			break
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					story.WriteString(part.Text)
				}
			}
		}
	}

	if lastErr != nil {
		return "", lastErr
	}

	return story.String(), nil
}

// =============================================================================
// Text Chunking
// =============================================================================

// ChunkConfig defines how to split text
type ChunkConfig struct {
	Name       string
	SizeChars  int // 0 means no chunking (full document)
	OverlapPct int // Overlap as percentage of chunk size
}

// TextChunk represents a chunk of text
type TextChunk struct {
	Index    int
	Text     string
	StartPos int
	EndPos   int
}

// chunkText splits text into chunks with overlap
func chunkText(text string, config ChunkConfig) []TextChunk {
	if config.SizeChars == 0 || len(text) <= config.SizeChars {
		return []TextChunk{{Index: 0, Text: text, StartPos: 0, EndPos: len(text)}}
	}

	overlapChars := (config.SizeChars * config.OverlapPct) / 100
	stepSize := config.SizeChars - overlapChars

	var chunks []TextChunk
	for i := 0; i < len(text); i += stepSize {
		end := i + config.SizeChars
		if end > len(text) {
			end = len(text)
		}

		// Try to break at sentence boundary
		chunkText := text[i:end]
		if end < len(text) {
			lastPeriod := strings.LastIndex(chunkText, ". ")
			if lastPeriod > config.SizeChars/2 {
				chunkText = chunkText[:lastPeriod+1]
				end = i + lastPeriod + 1
			}
		}

		chunks = append(chunks, TextChunk{
			Index:    len(chunks),
			Text:     chunkText,
			StartPos: i,
			EndPos:   end,
		})

		if end >= len(text) {
			break
		}
	}

	return chunks
}

// =============================================================================
// Dynamic Extraction Schema Generation
// =============================================================================

// getEntitySchema returns a genai.Schema for entity extraction based on schema definition
func getEntitySchema(schema SchemaDefinition) *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"entities": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"id": {
							Type:        genai.TypeString,
							Description: "Unique identifier (e.g., 'person_1', 'place_1')",
						},
						"type": {
							Type:        genai.TypeString,
							Enum:        schema.EntityTypes,
							Description: "Entity type",
						},
						"name": {
							Type:        genai.TypeString,
							Description: "Full name",
						},
						"description": {
							Type:        genai.TypeString,
							Description: "Brief description",
						},
					},
					Required: []string{"id", "type", "name"},
				},
			},
		},
		Required: []string{"entities"},
	}
}

// getRelationshipSchema returns a genai.Schema for relationship extraction
func getRelationshipSchema(schema SchemaDefinition) *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"relationships": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"source_id": {
							Type:        genai.TypeString,
							Description: "ID of the source entity",
						},
						"target_id": {
							Type:        genai.TypeString,
							Description: "ID of the target entity",
						},
						"type": {
							Type:        genai.TypeString,
							Enum:        schema.RelationTypes,
							Description: "Relationship type",
						},
						"description": {
							Type:        genai.TypeString,
							Description: "Optional description",
						},
					},
					Required: []string{"source_id", "target_id", "type"},
				},
			},
		},
		Required: []string{"relationships"},
	}
}

// =============================================================================
// Entity Extraction
// =============================================================================

// ChunkExtractionResult holds results from extracting one chunk
type ChunkExtractionResult struct {
	ChunkIndex    int
	Entities      []TwoStepEntity
	Relationships []TwoStepRelationship
	Duration      time.Duration
	Error         error
}

// extractFromChunk extracts entities and relationships from a single chunk
func extractFromChunk(
	ctx context.Context,
	llm adkmodel.LLM,
	chunk TextChunk,
	chunkTotal int,
	schema SchemaDefinition,
) ChunkExtractionResult {
	result := ChunkExtractionResult{ChunkIndex: chunk.Index}
	start := time.Now()

	// Step 1: Entity extraction
	entityPrompt := buildEntityPromptForSchema(chunk.Text, chunk.Index, chunkTotal, schema)
	entityConfig := &genai.GenerateContentConfig{
		Temperature:      ptr(float32(0.0)),
		MaxOutputTokens:  8192,
		ResponseMIMEType: "application/json",
		ResponseSchema:   getEntitySchema(schema),
	}

	entityResponse, err := callChunkLLM(ctx, llm, entityPrompt, entityConfig)
	if err != nil {
		result.Error = fmt.Errorf("entity extraction failed: %w", err)
		result.Duration = time.Since(start)
		return result
	}

	var entitiesOutput TwoStepEntitiesOutput
	if err := json.Unmarshal([]byte(entityResponse), &entitiesOutput); err != nil {
		result.Error = fmt.Errorf("entity parse failed: %w", err)
		result.Duration = time.Since(start)
		return result
	}
	result.Entities = entitiesOutput.Entities

	// Step 2: Relationship extraction
	relPrompt := buildRelPromptForSchema(chunk.Text, entitiesOutput.Entities, chunk.Index, chunkTotal, schema)
	relConfig := &genai.GenerateContentConfig{
		Temperature:      ptr(float32(0.0)),
		MaxOutputTokens:  8192,
		ResponseMIMEType: "application/json",
		ResponseSchema:   getRelationshipSchema(schema),
	}

	relResponse, err := callChunkLLM(ctx, llm, relPrompt, relConfig)
	if err != nil {
		result.Error = fmt.Errorf("relationship extraction failed: %w", err)
		result.Duration = time.Since(start)
		return result
	}

	var relsOutput TwoStepRelationshipsOutput
	if err := json.Unmarshal([]byte(relResponse), &relsOutput); err != nil {
		result.Error = fmt.Errorf("relationship parse failed: %w", err)
		result.Duration = time.Since(start)
		return result
	}
	result.Relationships = relsOutput.Relationships

	result.Duration = time.Since(start)
	return result
}

func buildEntityPromptForSchema(text string, chunkIdx, chunkTotal int, schema SchemaDefinition) string {
	chunkInfo := ""
	if chunkTotal > 1 {
		chunkInfo = fmt.Sprintf("\n\nNote: This is chunk %d of %d. Extract all entities in THIS chunk.", chunkIdx+1, chunkTotal)
	}

	entityTypesList := strings.Join(schema.EntityTypes, ", ")

	return fmt.Sprintf(`Extract all entities from this text.

## Entity Types
%s

## Rules
1. Give each entity a unique ID (e.g., "person_1", "company_1")
2. Use FULL NAMES as they appear in the text
3. Include a brief description based on context%s

## Text
%s

Extract all entities as JSON.`, entityTypesList, chunkInfo, text)
}

func buildRelPromptForSchema(text string, entities []TwoStepEntity, chunkIdx, chunkTotal int, schema SchemaDefinition) string {
	entitiesJSON, _ := json.MarshalIndent(entities, "", "  ")

	chunkInfo := ""
	if chunkTotal > 1 {
		chunkInfo = fmt.Sprintf("\n\nNote: This is chunk %d of %d. Only create relationships in THIS chunk.", chunkIdx+1, chunkTotal)
	}

	relTypesList := strings.Join(schema.RelationTypes, ", ")

	return fmt.Sprintf(`Identify relationships between the extracted entities.

## Relationship Types
%s

## Rules
1. Only create relationships between entities in the provided list
2. Use exact entity IDs from the list
3. Only include relationships explicitly stated in the text%s

## Entities
%s

## Text
%s

Extract relationships as JSON.`, relTypesList, chunkInfo, string(entitiesJSON), text)
}

func callChunkLLM(ctx context.Context, llm adkmodel.LLM, prompt string, config *genai.GenerateContentConfig) (string, error) {
	llmRequest := &adkmodel.LLMRequest{
		Contents: []*genai.Content{
			{
				Role:  "user",
				Parts: []*genai.Part{{Text: prompt}},
			},
		},
		Config: config,
	}

	var responseText strings.Builder
	var lastErr error

	for resp, err := range llm.GenerateContent(ctx, llmRequest, false) {
		if err != nil {
			lastErr = err
			break
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
			}
		}
	}

	if lastErr != nil {
		return "", lastErr
	}

	return cleanComparisonJSONResponse(responseText.String()), nil
}

// =============================================================================
// Entity Deduplication
// =============================================================================

// MergedResults holds deduplicated extraction results
type MergedResults struct {
	Entities      []TwoStepEntity
	Relationships []TwoStepRelationship
	EntityNameMap map[string]string
}

// mergeChunkResults deduplicates and merges results from multiple chunks
func mergeChunkResults(results []ChunkExtractionResult) MergedResults {
	merged := MergedResults{
		EntityNameMap: make(map[string]string),
	}

	entityByName := make(map[string]TwoStepEntity)
	relSet := make(map[string]TwoStepRelationship)

	for _, result := range results {
		if result.Error != nil {
			continue
		}

		for _, e := range result.Entities {
			normName := normalizeEntityName(e.Name)
			if existing, found := entityByName[normName]; found {
				if len(e.Description) > len(existing.Description) {
					existing.Description = e.Description
					entityByName[normName] = existing
				}
			} else {
				newID := fmt.Sprintf("%s_%d", strings.ToLower(e.Type), len(entityByName)+1)
				e.ID = newID
				entityByName[normName] = e
				merged.EntityNameMap[normName] = newID
			}
		}

		for _, r := range result.Relationships {
			sourceID := findCanonicalID(r.SourceID, results, merged.EntityNameMap)
			targetID := findCanonicalID(r.TargetID, results, merged.EntityNameMap)

			if sourceID == "" || targetID == "" {
				continue
			}

			relKey := fmt.Sprintf("%s|%s|%s", sourceID, r.Type, targetID)
			if _, exists := relSet[relKey]; !exists {
				relSet[relKey] = TwoStepRelationship{
					SourceID:    sourceID,
					TargetID:    targetID,
					Type:        r.Type,
					Description: r.Description,
				}
			}
		}
	}

	for _, e := range entityByName {
		merged.Entities = append(merged.Entities, e)
	}
	for _, r := range relSet {
		merged.Relationships = append(merged.Relationships, r)
	}

	return merged
}

func normalizeEntityName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func findCanonicalID(id string, results []ChunkExtractionResult, nameMap map[string]string) string {
	for _, result := range results {
		for _, e := range result.Entities {
			if e.ID == id {
				normName := normalizeEntityName(e.Name)
				if canonicalID, found := nameMap[normName]; found {
					return canonicalID
				}
			}
		}
	}
	return ""
}

// =============================================================================
// Metrics Calculation
// =============================================================================

// ChunkSchemaMetrics holds metrics for one test configuration
type ChunkSchemaMetrics struct {
	SchemaComplexity   SchemaComplexity
	ChunkConfig        string
	ChunkCount         int
	TotalDuration      time.Duration
	EntityCount        int
	RelationshipCount  int
	EntityPrecision    float64
	EntityRecall       float64
	RelationshipRecall float64
	OrphanRate         float64
	DuplicateEntities  int
	UniqueEntities     int
}

// calculateChunkSchemaMetrics compares extraction results against ground truth
func calculateChunkSchemaMetrics(
	schemaComplexity SchemaComplexity,
	chunkConfigName string,
	chunks []TextChunk,
	results []ChunkExtractionResult,
	merged MergedResults,
	groundTruth *GroundTruth,
) ChunkSchemaMetrics {
	metrics := ChunkSchemaMetrics{
		SchemaComplexity:  schemaComplexity,
		ChunkConfig:       chunkConfigName,
		ChunkCount:        len(chunks),
		EntityCount:       len(merged.Entities),
		RelationshipCount: len(merged.Relationships),
	}

	for _, r := range results {
		metrics.TotalDuration += r.Duration
	}

	for _, r := range results {
		metrics.DuplicateEntities += len(r.Entities)
	}
	metrics.UniqueEntities = len(merged.Entities)

	// Build ground truth name set
	gtEntities := make(map[string]bool)
	for _, e := range groundTruth.Entities {
		gtEntities[normalizeEntityName(e.Name)] = true
	}

	// Count matched entities
	extractedEntities := make(map[string]bool)
	matchedEntities := 0
	for _, e := range merged.Entities {
		normName := normalizeEntityName(e.Name)
		extractedEntities[normName] = true
		if gtEntities[normName] {
			matchedEntities++
		}
	}

	if len(extractedEntities) > 0 {
		metrics.EntityPrecision = float64(matchedEntities) / float64(len(extractedEntities))
	}
	if len(gtEntities) > 0 {
		metrics.EntityRecall = float64(matchedEntities) / float64(len(gtEntities))
	}

	// Calculate relationship recall
	gtRelSet := make(map[string]bool)
	for _, r := range groundTruth.Relationships {
		key := fmt.Sprintf("%s|%s|%s",
			normalizeEntityName(r.SourceName),
			r.Type,
			normalizeEntityName(r.TargetName))
		gtRelSet[key] = true
	}

	matchedRels := 0
	for _, r := range merged.Relationships {
		sourceName := findEntityName(r.SourceID, merged.Entities)
		targetName := findEntityName(r.TargetID, merged.Entities)
		key := fmt.Sprintf("%s|%s|%s",
			normalizeEntityName(sourceName),
			r.Type,
			normalizeEntityName(targetName))
		if gtRelSet[key] {
			matchedRels++
		}
	}

	if len(gtRelSet) > 0 {
		metrics.RelationshipRecall = float64(matchedRels) / float64(len(gtRelSet))
	}

	// Calculate orphan rate
	connected := make(map[string]bool)
	for _, r := range merged.Relationships {
		connected[r.SourceID] = true
		connected[r.TargetID] = true
	}
	orphanCount := 0
	for _, e := range merged.Entities {
		if !connected[e.ID] {
			orphanCount++
		}
	}
	if len(merged.Entities) > 0 {
		metrics.OrphanRate = float64(orphanCount) / float64(len(merged.Entities))
	}

	return metrics
}

func findEntityName(id string, entities []TwoStepEntity) string {
	for _, e := range entities {
		if e.ID == id {
			return e.Name
		}
	}
	return ""
}

// =============================================================================
// Main Test
// =============================================================================

// TestChunkAndSchemaComparison tests extraction quality across chunk sizes AND schema complexities
func TestChunkAndSchemaComparison(t *testing.T) {
	projectID := os.Getenv("VERTEX_PROJECT_ID")
	if projectID == "" {
		t.Skip("VERTEX_PROJECT_ID not set, skipping E2E test")
	}

	ctx := context.Background()

	// Create LLM
	llmConfig := &config.LLMConfig{
		GCPProjectID:     projectID,
		VertexAILocation: "us-central1",
		Model:            "gemini-2.0-flash",
		MaxOutputTokens:  8192,
		Temperature:      0,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	modelFactory := adk.NewModelFactory(llmConfig, logger)

	llm, err := modelFactory.CreateModel(ctx)
	require.NoError(t, err, "Failed to create model")

	// Define ground truths for each schema complexity
	groundTruths := map[SchemaComplexity]*GroundTruth{
		SchemaSimple:  createSimpleGroundTruth(),
		SchemaMedium:  createMediumGroundTruth(),
		SchemaComplex: createComplexGroundTruth(),
	}

	// Define chunk configurations
	chunkConfigs := []ChunkConfig{
		{Name: "3k", SizeChars: 3000, OverlapPct: 10},
		{Name: "6k", SizeChars: 6000, OverlapPct: 10},
		{Name: "12k", SizeChars: 12000, OverlapPct: 10},
		{Name: "full", SizeChars: 0, OverlapPct: 0},
	}

	// Run tests for each schema complexity
	var allMetrics []ChunkSchemaMetrics

	for _, complexity := range []SchemaComplexity{SchemaSimple, SchemaMedium, SchemaComplex} {
		groundTruth := groundTruths[complexity]

		t.Log("\n" + strings.Repeat("=", 80))
		t.Logf("SCHEMA: %s (%d entity types, %d relationship types)",
			complexity, len(groundTruth.Schema.EntityTypes), len(groundTruth.Schema.RelationTypes))
		t.Logf("Ground truth: %d entities, %d relationships",
			len(groundTruth.Entities), len(groundTruth.Relationships))
		t.Log(strings.Repeat("=", 80))

		// Generate or load story
		storyPath := fmt.Sprintf("/tmp/chunk_test_story_%s.txt", complexity)
		var story string

		if data, err := os.ReadFile(storyPath); err == nil && len(data) > 5000 {
			t.Logf("Using cached story for %s", complexity)
			story = string(data)
		} else {
			t.Logf("Generating story for %s...", complexity)
			start := time.Now()
			story, err = generateStory(ctx, llm, groundTruth)
			require.NoError(t, err, "Failed to generate story")
			t.Logf("Generated: %d characters in %v", len(story), time.Since(start))
			_ = os.WriteFile(storyPath, []byte(story), 0644)
		}

		t.Logf("Story length: %d chars (~%d pages)", len(story), len(story)/3000)

		// Test each chunk configuration
		for _, chunkCfg := range chunkConfigs {
			t.Logf("\n--- Chunk config: %s ---", chunkCfg.Name)

			chunks := chunkText(story, chunkCfg)
			t.Logf("Chunks: %d", len(chunks))

			results := make([]ChunkExtractionResult, len(chunks))
			for j, chunk := range chunks {
				t.Logf("  Chunk %d/%d (%d chars)...", j+1, len(chunks), len(chunk.Text))
				results[j] = extractFromChunk(ctx, llm, chunk, len(chunks), groundTruth.Schema)
				if results[j].Error != nil {
					t.Logf("    ERROR: %v", results[j].Error)
				} else {
					t.Logf("    Found %d entities, %d rels in %v",
						len(results[j].Entities), len(results[j].Relationships), results[j].Duration)
				}
			}

			merged := mergeChunkResults(results)
			t.Logf("After dedup: %d entities, %d relationships",
				len(merged.Entities), len(merged.Relationships))

			metrics := calculateChunkSchemaMetrics(complexity, chunkCfg.Name, chunks, results, merged, groundTruth)
			allMetrics = append(allMetrics, metrics)
		}
	}

	// Print final comparison table
	t.Log("\n" + strings.Repeat("=", 130))
	t.Log("CHUNK SIZE × SCHEMA COMPLEXITY COMPARISON")
	t.Log(strings.Repeat("=", 130))
	t.Logf("%-8s | %-6s | %6s | %8s | %8s | %10s | %9s | %9s | %8s | %8s",
		"Schema", "Chunks", "Count", "Entities", "Rels", "Duration", "E-Prec", "E-Recall", "R-Recall", "Orphan%")
	t.Log(strings.Repeat("-", 130))

	for _, m := range allMetrics {
		t.Logf("%-8s | %-6s | %6d | %8d | %8d | %10v | %8.1f%% | %8.1f%% | %7.1f%% | %7.1f%%",
			m.SchemaComplexity,
			m.ChunkConfig,
			m.ChunkCount,
			m.EntityCount,
			m.RelationshipCount,
			m.TotalDuration.Round(time.Millisecond),
			m.EntityPrecision*100,
			m.EntityRecall*100,
			m.RelationshipRecall*100,
			m.OrphanRate*100)
	}
	t.Log(strings.Repeat("=", 130))

	// Print summary by schema complexity
	t.Log("\nSUMMARY BY SCHEMA COMPLEXITY (averaged across chunk sizes):")
	for _, complexity := range []SchemaComplexity{SchemaSimple, SchemaMedium, SchemaComplex} {
		var avgPrecision, avgRecall, avgRelRecall float64
		var count int
		for _, m := range allMetrics {
			if m.SchemaComplexity == complexity {
				avgPrecision += m.EntityPrecision
				avgRecall += m.EntityRecall
				avgRelRecall += m.RelationshipRecall
				count++
			}
		}
		if count > 0 {
			t.Logf("  %s: Entity Precision=%.1f%%, Entity Recall=%.1f%%, Rel Recall=%.1f%%",
				complexity,
				avgPrecision/float64(count)*100,
				avgRecall/float64(count)*100,
				avgRelRecall/float64(count)*100)
		}
	}
}

// Helper function to create pointer
func ptr[T any](v T) *T {
	return &v
}

// =============================================================================
// SINGLE-STEP VS TWO-STEP COMPARISON TEST
// =============================================================================

type ExtractionStrategy string

const (
	StrategySingleStep ExtractionStrategy = "single"
	StrategyTwoStep    ExtractionStrategy = "two-step"
)

type SingleStepOutput struct {
	Entities      []TwoStepEntity       `json:"entities"`
	Relationships []TwoStepRelationship `json:"relationships"`
}

func extractFromChunkSingleStep(
	ctx context.Context,
	llm adkmodel.LLM,
	chunk TextChunk,
	chunkTotal int,
	schema SchemaDefinition,
) ChunkExtractionResult {
	return extractFromChunkSingleStepWithTokens(ctx, llm, chunk, chunkTotal, schema, 8192)
}

func extractFromChunkSingleStepWithTokens(
	ctx context.Context,
	llm adkmodel.LLM,
	chunk TextChunk,
	chunkTotal int,
	schema SchemaDefinition,
	maxTokens int32,
) ChunkExtractionResult {
	result := ChunkExtractionResult{ChunkIndex: chunk.Index}
	start := time.Now()

	prompt := buildSingleStepPrompt(chunk.Text, chunk.Index, chunkTotal, schema)
	config := &genai.GenerateContentConfig{
		Temperature:      ptr(float32(0.0)),
		MaxOutputTokens:  maxTokens,
		ResponseMIMEType: "application/json",
		ResponseSchema:   getSingleStepSchema(schema),
	}

	response, err := callChunkLLM(ctx, llm, prompt, config)
	if err != nil {
		result.Error = fmt.Errorf("single-step extraction failed: %w", err)
		result.Duration = time.Since(start)
		return result
	}

	var output SingleStepOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		result.Error = fmt.Errorf("single-step parse failed: %w (response length: %d, preview: %.200s)", err, len(response), response)
		result.Duration = time.Since(start)
		return result
	}

	result.Entities = output.Entities
	result.Relationships = output.Relationships
	result.Duration = time.Since(start)
	return result
}

func buildSingleStepPrompt(text string, chunkIdx, chunkTotal int, schema SchemaDefinition) string {
	chunkInfo := ""
	if chunkTotal > 1 {
		chunkInfo = fmt.Sprintf("\n\nNote: This is chunk %d of %d. Extract all entities and relationships in THIS chunk.", chunkIdx+1, chunkTotal)
	}

	entityTypesList := strings.Join(schema.EntityTypes, ", ")
	relTypesList := strings.Join(schema.RelationTypes, ", ")

	return fmt.Sprintf(`Extract all entities and their relationships from this text in a single pass.

## Entity Types
%s

## Relationship Types
%s

## Rules
1. Extract entities with unique IDs, names, and types
2. Extract relationships between entities using their IDs
3. Only include entities and relationships explicitly stated in the text
4. Each entity needs a unique ID (e.g., "person_1", "place_1")%s

## Text
%s

Extract entities and relationships as JSON.`, entityTypesList, relTypesList, chunkInfo, text)
}

func getSingleStepSchema(schema SchemaDefinition) *genai.Schema {
	entityEnumVals := make([]string, len(schema.EntityTypes))
	copy(entityEnumVals, schema.EntityTypes)

	relEnumVals := make([]string, len(schema.RelationTypes))
	copy(relEnumVals, schema.RelationTypes)

	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"entities": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"id":   {Type: genai.TypeString},
						"name": {Type: genai.TypeString},
						"type": {Type: genai.TypeString, Enum: entityEnumVals},
					},
					Required: []string{"id", "name", "type"},
				},
			},
			"relationships": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"source_id": {Type: genai.TypeString},
						"target_id": {Type: genai.TypeString},
						"type":      {Type: genai.TypeString, Enum: relEnumVals},
					},
					Required: []string{"source_id", "target_id", "type"},
				},
			},
		},
		Required: []string{"entities", "relationships"},
	}
}

type StrategyMetrics struct {
	Strategy           ExtractionStrategy
	SchemaComplexity   SchemaComplexity
	ChunkConfig        string
	ChunkCount         int
	EntityCount        int
	RelationshipCount  int
	TotalDuration      time.Duration
	EntityPrecision    float64
	EntityRecall       float64
	RelationshipRecall float64
	OrphanRate         float64
}

func TestSingleStepVsTwoStepComparison(t *testing.T) {
	if os.Getenv("VERTEX_PROJECT_ID") == "" {
		t.Skip("VERTEX_PROJECT_ID not set")
	}

	ctx := context.Background()
	projectID := os.Getenv("VERTEX_PROJECT_ID")

	llmConfig := &config.LLMConfig{
		GCPProjectID:     projectID,
		VertexAILocation: "us-central1",
		Model:            "gemini-2.0-flash",
		MaxOutputTokens:  8192,
		Temperature:      0,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	modelFactory := adk.NewModelFactory(llmConfig, logger)

	llm, err := modelFactory.CreateModel(ctx)
	require.NoError(t, err, "Failed to create model")

	groundTruths := map[SchemaComplexity]*GroundTruth{
		SchemaSimple:  createSimpleGroundTruth(),
		SchemaMedium:  createMediumGroundTruth(),
		SchemaComplex: createComplexGroundTruth(),
	}

	chunkConfigs := []ChunkConfig{
		{Name: "3k", SizeChars: 3000, OverlapPct: 10},
		{Name: "6k", SizeChars: 6000, OverlapPct: 10},
		{Name: "12k", SizeChars: 12000, OverlapPct: 10},
		{Name: "full", SizeChars: 0, OverlapPct: 0},
	}

	strategies := []ExtractionStrategy{StrategySingleStep, StrategyTwoStep}

	var allMetrics []StrategyMetrics

	for _, complexity := range []SchemaComplexity{SchemaSimple, SchemaMedium, SchemaComplex} {
		groundTruth := groundTruths[complexity]

		t.Log("\n" + strings.Repeat("=", 100))
		t.Logf("SCHEMA: %s (%d entity types, %d relationship types)",
			complexity, len(groundTruth.Schema.EntityTypes), len(groundTruth.Schema.RelationTypes))
		t.Logf("Ground truth: %d entities, %d relationships",
			len(groundTruth.Entities), len(groundTruth.Relationships))
		t.Log(strings.Repeat("=", 100))

		storyPath := fmt.Sprintf("/tmp/chunk_test_story_%s.txt", complexity)
		data, err := os.ReadFile(storyPath)
		require.NoError(t, err, "Cached story not found at %s - run TestChunkAndSchemaComparison first", storyPath)
		story := string(data)

		t.Logf("Using cached story: %d chars (~%d pages)", len(story), len(story)/3000)

		for _, chunkCfg := range chunkConfigs {
			chunks := chunkText(story, chunkCfg)

			for _, strategy := range strategies {
				t.Logf("\n--- %s | %s | %d chunks ---", complexity, strategy, len(chunks))

				results := make([]ChunkExtractionResult, len(chunks))
				for j, chunk := range chunks {
					t.Logf("  Chunk %d/%d (%d chars)...", j+1, len(chunks), len(chunk.Text))

					if strategy == StrategySingleStep {
						results[j] = extractFromChunkSingleStep(ctx, llm, chunk, len(chunks), groundTruth.Schema)
					} else {
						results[j] = extractFromChunk(ctx, llm, chunk, len(chunks), groundTruth.Schema)
					}

					if results[j].Error != nil {
						t.Logf("    ERROR: %v", results[j].Error)
					} else {
						t.Logf("    Found %d entities, %d rels in %v",
							len(results[j].Entities), len(results[j].Relationships), results[j].Duration)
					}
				}

				merged := mergeChunkResults(results)
				t.Logf("After dedup: %d entities, %d relationships",
					len(merged.Entities), len(merged.Relationships))

				baseMetrics := calculateChunkSchemaMetrics(complexity, chunkCfg.Name, chunks, results, merged, groundTruth)

				metrics := StrategyMetrics{
					Strategy:           strategy,
					SchemaComplexity:   baseMetrics.SchemaComplexity,
					ChunkConfig:        baseMetrics.ChunkConfig,
					ChunkCount:         baseMetrics.ChunkCount,
					EntityCount:        baseMetrics.EntityCount,
					RelationshipCount:  baseMetrics.RelationshipCount,
					TotalDuration:      baseMetrics.TotalDuration,
					EntityPrecision:    baseMetrics.EntityPrecision,
					EntityRecall:       baseMetrics.EntityRecall,
					RelationshipRecall: baseMetrics.RelationshipRecall,
					OrphanRate:         baseMetrics.OrphanRate,
				}
				allMetrics = append(allMetrics, metrics)
			}
		}
	}

	// Print comparison table
	t.Log("\n" + strings.Repeat("=", 150))
	t.Log("SINGLE-STEP VS TWO-STEP EXTRACTION COMPARISON")
	t.Log(strings.Repeat("=", 150))
	t.Logf("%-8s | %-8s | %-6s | %6s | %8s | %8s | %10s | %9s | %9s | %9s | %8s",
		"Schema", "Strategy", "Chunks", "Count", "Entities", "Rels", "Duration", "E-Prec", "E-Recall", "R-Recall", "Orphan%")
	t.Log(strings.Repeat("-", 150))

	for _, m := range allMetrics {
		t.Logf("%-8s | %-8s | %-6s | %6d | %8d | %8d | %10v | %8.1f%% | %8.1f%% | %8.1f%% | %7.1f%%",
			m.SchemaComplexity,
			m.Strategy,
			m.ChunkConfig,
			m.ChunkCount,
			m.EntityCount,
			m.RelationshipCount,
			m.TotalDuration.Round(time.Millisecond),
			m.EntityPrecision*100,
			m.EntityRecall*100,
			m.RelationshipRecall*100,
			m.OrphanRate*100)
	}
	t.Log(strings.Repeat("=", 150))

	// Print summary comparing strategies
	t.Log("\nSUMMARY BY STRATEGY (averaged across all configurations):")
	for _, strategy := range strategies {
		var avgPrecision, avgRecall, avgRelRecall, avgDuration float64
		var count int
		for _, m := range allMetrics {
			if m.Strategy == strategy {
				avgPrecision += m.EntityPrecision
				avgRecall += m.EntityRecall
				avgRelRecall += m.RelationshipRecall
				avgDuration += m.TotalDuration.Seconds()
				count++
			}
		}
		if count > 0 {
			t.Logf("  %s: E-Precision=%.1f%%, E-Recall=%.1f%%, R-Recall=%.1f%%, Avg Duration=%.1fs",
				strategy,
				avgPrecision/float64(count)*100,
				avgRecall/float64(count)*100,
				avgRelRecall/float64(count)*100,
				avgDuration/float64(count))
		}
	}

	// Print delta (two-step vs single-step)
	t.Log("\nDELTA (Two-Step minus Single-Step) by Schema+Chunk:")
	for _, complexity := range []SchemaComplexity{SchemaSimple, SchemaMedium, SchemaComplex} {
		for _, chunkCfg := range chunkConfigs {
			var single, twoStep *StrategyMetrics
			for i := range allMetrics {
				m := &allMetrics[i]
				if m.SchemaComplexity == complexity && m.ChunkConfig == chunkCfg.Name {
					if m.Strategy == StrategySingleStep {
						single = m
					} else {
						twoStep = m
					}
				}
			}
			if single != nil && twoStep != nil {
				t.Logf("  %s/%s: E-Prec %+.1f%%, E-Recall %+.1f%%, R-Recall %+.1f%%, Duration %+.1fs",
					complexity, chunkCfg.Name,
					(twoStep.EntityPrecision-single.EntityPrecision)*100,
					(twoStep.EntityRecall-single.EntityRecall)*100,
					(twoStep.RelationshipRecall-single.RelationshipRecall)*100,
					twoStep.TotalDuration.Seconds()-single.TotalDuration.Seconds())
			}
		}
	}
}

// =============================================================================
// MODEL COMPARISON TEST (2.0-flash vs 2.5-flash)
// =============================================================================

type ModelMetrics struct {
	Model              string
	SchemaComplexity   SchemaComplexity
	ChunkConfig        string
	ChunkCount         int
	EntityCount        int
	RelationshipCount  int
	TotalDuration      time.Duration
	EntityPrecision    float64
	EntityRecall       float64
	RelationshipRecall float64
	OrphanRate         float64
}

func TestModelComparison(t *testing.T) {
	if os.Getenv("VERTEX_PROJECT_ID") == "" {
		t.Skip("VERTEX_PROJECT_ID not set")
	}

	ctx := context.Background()
	projectID := os.Getenv("VERTEX_PROJECT_ID")

	models := []string{
		"gemini-2.0-flash",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
		"gemini-3-flash-preview",
		"gemini-3-pro-preview",
	}

	groundTruth := createMediumGroundTruth()
	chunkCfg := ChunkConfig{Name: "6k", SizeChars: 6000, OverlapPct: 10}

	storyPath := "/tmp/chunk_test_story_medium.txt"
	data, err := os.ReadFile(storyPath)
	require.NoError(t, err, "Cached story not found - run TestChunkAndSchemaComparison first")
	story := string(data)

	t.Logf("Using cached medium story: %d chars (~%d pages)", len(story), len(story)/3000)
	t.Logf("Ground truth: %d entities, %d relationships", len(groundTruth.Entities), len(groundTruth.Relationships))

	chunks := chunkText(story, chunkCfg)
	t.Logf("Chunks: %d (6k size)", len(chunks))

	var allMetrics []ModelMetrics

	for _, modelName := range models {
		t.Log("\n" + strings.Repeat("=", 80))
		t.Logf("MODEL: %s", modelName)
		t.Log(strings.Repeat("=", 80))

		// Use global endpoint for all models (required for Gemini 3)
		llmConfig := &config.LLMConfig{
			GCPProjectID:     projectID,
			VertexAILocation: "global",
			Model:            modelName,
			MaxOutputTokens:  8192,
			Temperature:      0,
		}

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
		modelFactory := adk.NewModelFactory(llmConfig, logger)

		llm, err := modelFactory.CreateModel(ctx)
		require.NoError(t, err, "Failed to create model %s", modelName)

		results := make([]ChunkExtractionResult, len(chunks))
		for j, chunk := range chunks {
			t.Logf("  Chunk %d/%d (%d chars)...", j+1, len(chunks), len(chunk.Text))

			// Gemini 2.5+ and 3.x are "thinking" models that need higher token limits
			maxTokens := int32(8192)
			if strings.Contains(modelName, "2.5") || strings.Contains(modelName, "3-") {
				maxTokens = 65536
			}
			results[j] = extractFromChunkSingleStepWithTokens(ctx, llm, chunk, len(chunks), groundTruth.Schema, maxTokens)

			if results[j].Error != nil {
				t.Logf("    ERROR: %v", results[j].Error)
			} else {
				t.Logf("    Found %d entities, %d rels in %v",
					len(results[j].Entities), len(results[j].Relationships), results[j].Duration)
			}
		}

		merged := mergeChunkResults(results)
		t.Logf("After dedup: %d entities, %d relationships", len(merged.Entities), len(merged.Relationships))

		baseMetrics := calculateChunkSchemaMetrics(SchemaMedium, chunkCfg.Name, chunks, results, merged, groundTruth)

		metrics := ModelMetrics{
			Model:              modelName,
			SchemaComplexity:   SchemaMedium,
			ChunkConfig:        chunkCfg.Name,
			ChunkCount:         baseMetrics.ChunkCount,
			EntityCount:        baseMetrics.EntityCount,
			RelationshipCount:  baseMetrics.RelationshipCount,
			TotalDuration:      baseMetrics.TotalDuration,
			EntityPrecision:    baseMetrics.EntityPrecision,
			EntityRecall:       baseMetrics.EntityRecall,
			RelationshipRecall: baseMetrics.RelationshipRecall,
			OrphanRate:         baseMetrics.OrphanRate,
		}
		allMetrics = append(allMetrics, metrics)
	}

	t.Log("\n" + strings.Repeat("=", 130))
	t.Log("MODEL COMPARISON (Medium Schema, 6k Chunks, Single-Step)")
	t.Log(strings.Repeat("=", 130))
	t.Logf("%-35s | %6s | %8s | %8s | %10s | %9s | %9s | %9s | %8s",
		"Model", "Chunks", "Entities", "Rels", "Duration", "E-Prec", "E-Recall", "R-Recall", "Orphan%")
	t.Log(strings.Repeat("-", 130))

	for _, m := range allMetrics {
		t.Logf("%-35s | %6d | %8d | %8d | %10v | %8.1f%% | %8.1f%% | %8.1f%% | %7.1f%%",
			m.Model,
			m.ChunkCount,
			m.EntityCount,
			m.RelationshipCount,
			m.TotalDuration.Round(time.Millisecond),
			m.EntityPrecision*100,
			m.EntityRecall*100,
			m.RelationshipRecall*100,
			m.OrphanRate*100)
	}
	t.Log(strings.Repeat("=", 130))

	if len(allMetrics) >= 2 {
		base := allMetrics[0]
		for i := 1; i < len(allMetrics); i++ {
			m := allMetrics[i]
			t.Logf("\nDELTA (%s vs %s):", m.Model, base.Model)
			t.Logf("  E-Precision: %+.1f%%", (m.EntityPrecision-base.EntityPrecision)*100)
			t.Logf("  E-Recall: %+.1f%%", (m.EntityRecall-base.EntityRecall)*100)
			t.Logf("  R-Recall: %+.1f%%", (m.RelationshipRecall-base.RelationshipRecall)*100)
			t.Logf("  Duration: %+.1fs", m.TotalDuration.Seconds()-base.TotalDuration.Seconds())
		}
	}
}
