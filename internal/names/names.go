// Package names provides human-readable name assignment for workers.
// Names are generated using a Docker-like approach: adjective + noun combinations.
// This provides a large variety of unique, memorable names.
package names

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
)

// adjectives is a large list of adjectives for name generation.
// Inspired by Docker's naming scheme.
var adjectives = []string{
	"admiring", "adoring", "affectionate", "agile", "amazing",
	"ambitious", "amusing", "ancient", "artful", "awesome",
	"beautiful", "beloved", "blazing", "bold", "brave",
	"bright", "brilliant", "calm", "capable", "careful",
	"charming", "cheerful", "clever", "colorful", "confident",
	"cool", "courageous", "creative", "curious", "daring",
	"dazzling", "determined", "diligent", "dreamy", "eager",
	"earnest", "ecstatic", "elastic", "elegant", "eloquent",
	"elusive", "enchanted", "energetic", "epic", "ethereal",
	"excellent", "exciting", "expert", "fabulous", "fair",
	"faithful", "famous", "fancy", "fantastic", "fearless",
	"fervent", "festive", "fierce", "fiery", "flamboyant",
	"flying", "focused", "friendly", "frosty", "funny",
	"gallant", "generous", "gentle", "gifted", "gleaming",
	"glorious", "golden", "graceful", "gracious", "grand",
	"great", "handsome", "happy", "hardy", "harmonious",
	"heartfelt", "heroic", "hidden", "hopeful", "humble",
	"ideal", "imaginative", "immense", "incredible", "industrious",
	"ingenious", "innocent", "inspiring", "intrepid", "inventive",
	"jolly", "joyful", "joyous", "jubilant", "keen",
	"kind", "laughing", "legendary", "lively", "logical",
	"loving", "loyal", "lucky", "luminous", "magical",
	"magnificent", "majestic", "marvelous", "masterful", "mellow",
	"memorable", "merry", "mighty", "mindful", "mirthful",
	"modest", "mystical", "nimble", "noble", "nostalgic",
	"observant", "optimistic", "outstanding", "patient", "peaceful",
	"perfect", "persistent", "philosophical", "playful", "pleasant",
	"plucky", "poised", "polished", "powerful", "practical",
	"precious", "productive", "proud", "prudent", "quick",
	"quiet", "radiant", "regal", "relaxed", "remarkable",
	"resilient", "resolute", "resourceful", "reverent", "robust",
	"romantic", "royal", "sage", "serene", "sharp",
	"shining", "silent", "sincere", "skilled", "sleek",
	"smart", "smooth", "snappy", "soaring", "sparkling",
	"spectacular", "spirited", "splendid", "stalwart", "steadfast",
	"stellar", "stoic", "striking", "strong", "stunning",
	"sublime", "sunny", "superb", "swift", "talented",
	"tenacious", "tender", "terrific", "thoughtful", "thriving",
	"tidy", "tranquil", "tremendous", "trusty", "truthful",
	"unassuming", "upbeat", "valiant", "vibrant", "vigilant",
	"vigorous", "virtuous", "vivid", "warm", "watchful",
	"whimsical", "wild", "willing", "wise", "witty",
	"wonderful", "worthy", "youthful", "zealous", "zesty",
}

// nouns is a large list of nouns (famous scientists, pioneers, inventors) for name generation.
// Inspired by Docker's naming scheme.
var nouns = []string{
	"albattani", "allen", "archimedes", "ardinghelli", "aryabhata",
	"austin", "babbage", "banach", "bardeen", "bartik",
	"bassi", "beaver", "bell", "benz", "bhabha",
	"bhaskara", "blackwell", "bohr", "booth", "borg",
	"bose", "bouman", "boyd", "brahmagupta", "brattain",
	"brown", "buck", "burnell", "cannon", "carson",
	"cartwright", "cerf", "chandrasekhar", "chatelet", "chatterjee",
	"chebyshev", "clarke", "colden", "cori", "cray",
	"curie", "darwin", "davinci", "diffie", "dijkstra",
	"dirac", "driscoll", "dubinsky", "easley", "edison",
	"einstein", "elbakyan", "elgamal", "elion", "ellis",
	"engelbart", "euclid", "euler", "faraday", "feistel",
	"fermat", "fermi", "feynman", "franklin", "gagarin",
	"galileo", "galois", "ganguly", "gates", "gauss",
	"germain", "goldberg", "goldstine", "goldwasser", "golick",
	"goodall", "gould", "greider", "grothendieck", "hamilton",
	"haslett", "hawking", "heisenberg", "hellman", "hermann",
	"herschel", "hertz", "heyrovsky", "hodgkin", "hofstadter",
	"hoover", "hopper", "hugle", "hypatia", "ishizaka",
	"jackson", "jang", "jemison", "jennings", "jepsen",
	"johnson", "joliot", "jones", "kalam", "kapitsa",
	"kare", "keldysh", "keller", "kepler", "khayyam",
	"khorana", "kilby", "kirch", "knuth", "kowalevski",
	"lalande", "lamarr", "lamport", "leakey", "leavitt",
	"lederberg", "lehmann", "lewin", "lichterman", "liskov",
	"lovelace", "lumiere", "mahavira", "margulis", "matsumoto",
	"maxwell", "mayer", "mccarthy", "mcclintock", "mclaren",
	"mclean", "mcnulty", "meitner", "mendel", "mendeleev",
	"mirzakhani", "montalcini", "moore", "morse", "moser",
	"murdock", "napier", "nash", "neumann", "newton",
	"nightingale", "nobel", "noether", "northcutt", "noyce",
	"panini", "pare", "pascal", "pasteur", "payne",
	"perlman", "pike", "poincare", "poitras", "proskuriakova",
	"ptolemy", "raman", "ramanujan", "rhodes", "ride",
	"ritchie", "robinson", "roentgen", "rosalind", "rubin",
	"saha", "sammet", "sanderson", "satoshi", "shamir",
	"shannon", "shaw", "shirley", "shockley", "shtern",
	"sinoussi", "snyder", "solomon", "spence", "stonebraker",
	"sutherland", "swanson", "swartz", "swirles", "taussig",
	"tereshkova", "tesla", "tharp", "thompson", "torvalds",
	"turing", "varahamihira", "vaughan", "villani", "visvesvaraya",
	"volhard", "wescoff", "wilbur", "wiles", "williams",
	"wilson", "wing", "wozniak", "wright", "wu",
	"yalow", "yonath", "zhukovsky",
}

// DB is the interface required for name operations.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Generator is the interface for name generation operations.
//
//go:generate moq -stub -out names_mock.go . Generator:GeneratorMock
type Generator interface {
	GetNextAvailableName(ctx context.Context, db DB) (string, error)
}

// DefaultGenerator is the default implementation of the Generator interface.
type DefaultGenerator struct{}

// NewGenerator creates a new DefaultGenerator.
func NewGenerator() Generator {
	return &DefaultGenerator{}
}

// GetNextAvailableName implements Generator.
func (g *DefaultGenerator) GetNextAvailableName(ctx context.Context, db DB) (string, error) {
	return GetNextAvailableName(ctx, db)
}

// GenerateName creates a random name by combining an adjective and a noun.
// Format: "adjective_noun" (e.g., "brave_einstein", "clever_curie")
func GenerateName() string {
	adj := adjectives[rand.IntN(len(adjectives))]
	noun := nouns[rand.IntN(len(nouns))]
	return adj + "_" + noun
}

// GetNextAvailableName returns a unique name that is not currently in use.
// It generates random adjective_noun combinations until it finds one that's available.
// Returns empty string if it fails to generate a unique name after many attempts
// (extremely unlikely given the large combination space).
func GetNextAvailableName(ctx context.Context, db DB) (string, error) {
	// Get all currently used names
	usedNames, err := getUsedNames(ctx, db)
	if err != nil {
		return "", fmt.Errorf("failed to get used names: %w", err)
	}

	// Create a set of used names for O(1) lookup
	usedSet := make(map[string]bool)
	for _, name := range usedNames {
		usedSet[name] = true
	}

	// With 200 adjectives * 200 nouns = 40,000+ combinations,
	// collisions are extremely rare. Try up to 100 times.
	maxAttempts := 100
	for i := 0; i < maxAttempts; i++ {
		name := GenerateName()
		if !usedSet[name] {
			return name, nil
		}
	}

	// Extremely unlikely to reach here, but return empty string if we can't find a unique name
	return "", nil
}

// getUsedNames returns all names currently assigned to active works.
// A work is considered active if it's not completed or failed.
func getUsedNames(ctx context.Context, db DB) ([]string, error) {
	query := `
		SELECT name FROM works
		WHERE name != ''
		AND status NOT IN ('completed', 'failed')
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// ReleaseName is a no-op in the current implementation since names are automatically
// released when a work is completed or failed (they're excluded from the used names query).
// This function is provided for API completeness.
func ReleaseName(_ context.Context, _ DB, _ string) error {
	// Names are automatically released when work status changes to completed/failed
	return nil
}

// GetAllAdjectives returns the full list of adjectives.
func GetAllAdjectives() []string {
	return append([]string(nil), adjectives...)
}

// GetAllNouns returns the full list of nouns.
func GetAllNouns() []string {
	return append([]string(nil), nouns...)
}

// GetCombinationCount returns the total number of possible name combinations.
func GetCombinationCount() int {
	return len(adjectives) * len(nouns)
}

// ParseName splits a name into its adjective and noun components.
// Returns empty strings if the name is not in the expected format.
func ParseName(name string) (adjective, noun string) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
