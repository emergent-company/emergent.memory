package integration

// groundTruthEntity describes an expected entity with fuzzy matching.
type groundTruthEntity struct {
	Name      string
	TypeHints []string
}

// coreCastNames must always be extracted across all episodes.
var coreCastNames = []string{
	"Monica Geller", "Ross Geller", "Rachel Green",
	"Chandler Bing", "Joey Tribbiani", "Phoebe Buffay",
}

// groundTruthExpected lists all entities expected across 5 episodes.
var groundTruthExpected = []groundTruthEntity{
	{Name: "Monica Geller", TypeHints: []string{"Character", "Person"}},
	{Name: "Ross Geller", TypeHints: []string{"Character", "Person"}},
	{Name: "Rachel Green", TypeHints: []string{"Character", "Person"}},
	{Name: "Chandler Bing", TypeHints: []string{"Character", "Person"}},
	{Name: "Joey Tribbiani", TypeHints: []string{"Character", "Person"}},
	{Name: "Phoebe Buffay", TypeHints: []string{"Character", "Person"}},
	{Name: "Central Perk", TypeHints: []string{"Location"}},
	{Name: "Greenwich Village", TypeHints: []string{"Location"}},
	{Name: "Paul the Wine Guy", TypeHints: []string{"Character", "Person"}},
	{Name: "Carol", TypeHints: []string{"Character", "Person"}},
	{Name: "Susan", TypeHints: []string{"Character", "Person"}},
	{Name: "Barry Farber", TypeHints: []string{"Character", "Person"}},
	{Name: "Bloomingdale", TypeHints: []string{"Location"}},
	{Name: "Glenda", TypeHints: []string{"Character", "Person"}},
	{Name: "Madison Square Garden", TypeHints: []string{"Location"}},
	{Name: "Celeste", TypeHints: []string{"Character", "Person"}},
	{Name: "Alan", TypeHints: []string{"Character", "Person"}},
	{Name: "Laundromat", TypeHints: []string{"Location"}},
	{Name: "Museum of Prehistoric History", TypeHints: []string{"Location"}},
}

// epEntities groups entities by the episode they first appear in.
var epEntities = [][]groundTruthEntity{
	{
		{Name: "Monica Geller", TypeHints: []string{"Character", "Person"}},
		{Name: "Ross Geller", TypeHints: []string{"Character", "Person"}},
		{Name: "Rachel Green", TypeHints: []string{"Character", "Person"}},
		{Name: "Chandler Bing", TypeHints: []string{"Character", "Person"}},
		{Name: "Joey Tribbiani", TypeHints: []string{"Character", "Person"}},
		{Name: "Phoebe Buffay", TypeHints: []string{"Character", "Person"}},
		{Name: "Paul the Wine Guy", TypeHints: []string{"Character", "Person"}},
		{Name: "Central Perk", TypeHints: []string{"Location"}},
		{Name: "Greenwich Village", TypeHints: []string{"Location"}},
	},
	{
		{Name: "Carol", TypeHints: []string{"Character", "Person"}},
		{Name: "Susan", TypeHints: []string{"Character", "Person"}},
		{Name: "Barry Farber", TypeHints: []string{"Character", "Person"}},
		{Name: "Bloomingdale", TypeHints: []string{"Location"}},
	},
	{
		{Name: "Glenda", TypeHints: []string{"Character", "Person"}},
	},
	{
		{Name: "Celeste", TypeHints: []string{"Character", "Person"}},
		{Name: "Madison Square Garden", TypeHints: []string{"Location"}},
	},
	{
		{Name: "Alan", TypeHints: []string{"Character", "Person"}},
		{Name: "Laundromat", TypeHints: []string{"Location"}},
		{Name: "Museum of Prehistoric History", TypeHints: []string{"Location"}},
	},
}
