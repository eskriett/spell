package spell_test

import (
	"fmt"
	"sort"

	"github.com/eskriett/spell"
	"github.com/eskriett/strmet"
)

func ExampleSpell_AddEntry() {
	// Create a new speller
	s := spell.New()

	// Add a new word, "example" to the dictionary
	s.AddEntry(spell.Entry{
		Word:     "example",
		WordData: spell.WordData{"frequency": 10},
	})

	// Overwrite the data for word "example"
	s.AddEntry(spell.Entry{
		Word:     "example",
		WordData: spell.WordData{"frequency": 100},
	})

	// Output the frequency for word "example"
	entry := s.GetEntry("example")
	fmt.Printf("Output for word 'example' is: %v\n",
		entry.WordData.GetFrequency())
	// Output:
	// Output for word 'example' is: 100
}

func ExampleSpell_Lookup() {
	// Create a new speller
	s := spell.New()
	s.AddEntry(spell.Entry{
		Word:     "example",
		WordData: spell.WordData{"frequency": 1},
	})

	// Perform a default lookup for example
	suggestions, _ := s.Lookup("eample")
	fmt.Printf("Suggestions are: %v\n", suggestions)
	// Output:
	// Suggestions are: [example]
}

func ExampleSpell_Lookup_configureEditDistance() {
	// Create a new speller
	s := spell.New()
	s.AddEntry(spell.Entry{
		Word:     "example",
		WordData: spell.WordData{"frequency": 1},
	})

	// Lookup exact matches, i.e. edit distance = 0
	suggestions, _ := s.Lookup("eample", spell.EditDistance(0))
	fmt.Printf("Suggestions are: %v\n", suggestions)
	// Output:
	// Suggestions are: []
}

func ExampleSpell_Lookup_configureDistanceFunc() {
	// Create a new speller
	s := spell.New()
	s.AddEntry(spell.Entry{
		Word:     "example",
		WordData: spell.WordData{"frequency": 1},
	})

	// Configure the Lookup to use Levenshtein distance rather than the default
	// Damerau Levenshtein calculation
	s.Lookup("example", spell.DistanceFunc(func(s1, s2 string, maxDist int) int {
		// Call the Levenshtein function from github.com/eskriett/strmet
		return strmet.Levenshtein(s1, s2, maxDist)
	}))
}

func ExampleSpell_Lookup_configureSortFunc() {
	// Create a new speller
	s := spell.New()
	s.AddEntry(spell.Entry{
		Word:     "example",
		WordData: spell.WordData{"frequency": 1},
	})

	// Configure suggestions to be sorted solely by their frequency
	s.Lookup("example", spell.SortFunc(func(sl spell.SuggestionList) {
		sort.Slice(sl, func(i, j int) bool {
			s1Freq := sl[i].WordData.GetFrequency()
			s2Freq := sl[j].WordData.GetFrequency()
			return s1Freq < s2Freq
		})
	}))
}

func ExampleSpell_Segment() {
	// Create a new speller
	s := spell.New()

	wd := spell.WordData{"frequency": 1}
	s.AddEntry(spell.Entry{Word: "the", WordData: wd})
	s.AddEntry(spell.Entry{Word: "quick", WordData: wd})
	s.AddEntry(spell.Entry{Word: "brown", WordData: wd})
	s.AddEntry(spell.Entry{Word: "fox", WordData: wd})

	// Segment a string with word concatenated together
	segmentResult, _ := s.Segment("thequickbrownfox")
	fmt.Println(segmentResult)
	// Output:
	// the quick brown fox
}
