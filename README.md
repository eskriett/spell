# spell

[![GoDoc](https://godoc.org/github.com/eskriett/spell?status.svg)](https://godoc.org/github.com/eskriett/spell)
[![Go Report Card](https://goreportcard.com/badge/github.com/eskriett/spell)](https://goreportcard.com/report/github.com/eskriett/spell)
[![Build Status](https://travis-ci.com/eskriett/spell.svg?branch=master)](https://travis-ci.com/eskriett/spell)

A blazing fast spell checker written in Go.

__N.B.__ This library is still in early development and may change.

## Overview

```go
package main

import (
	"fmt"

	"github.com/eskriett/spell"
)

func main() {
	// Create a new instance of spell
	s := spell.New()

	// Add words to the dictionary. Words require a frequency, but can have
	// other arbitrary metadata associated with them
	s.AddEntry(spell.Entry{
		Frequency: 100,
		Word:      "two",
		WordData: spell.WordData{
			"type": "number",
		},
	})
	s.AddEntry(spell.Entry{
		Frequency: 1,
		Word:      "town",
		WordData: spell.WordData{
			"type": "noun",
		},
	})

	// Lookup a misspelling, by default the "best" suggestion will be returned
	suggestions, _ := s.Lookup("twon")
	fmt.Println(suggestions)
	// -> [two]

	suggestion := suggestions[0]

	// Get the frequency from the suggestion
	fmt.Println(suggestion.Frequency)
	// -> 100

	// Get metadata from the suggestion
	fmt.Println(suggestion.WordData["type"])
	// -> number

	// Get multiple suggestions during lookup
	suggestions, _ = s.Lookup("twon", spell.SuggestionLevel(spell.LevelAll))
	fmt.Println(suggestions)
	// -> [two, town]

	// Save the dictionary
	s.Save("dict.spell")

	// Load the dictionary
	s2, _ := spell.Load("dict.spell")

	suggestions, _ = s2.Lookup("twon", spell.SuggestionLevel(spell.LevelAll))
	fmt.Println(suggestions)
	// -> [two, town]

	// Spell supports word segmentation
	s3 := spell.New()

	s3.AddEntry(spell.Entry{Frequency: 1, Word: "the"})
	s3.AddEntry(spell.Entry{Frequency: 1, Word: "quick"})
	s3.AddEntry(spell.Entry{Frequency: 1, Word: "brown"})
	s3.AddEntry(spell.Entry{Frequency: 1, Word: "fox"})

	segmentResult, _ := s3.Segment("thequickbrownfox")
	fmt.Println(segmentResult)
	// -> the quick brown fox

	// Spell supports multiple dictionaries
	s4 := spell.New()

	s4.AddEntry(spell.Entry{Word: "épeler"}, spell.DictionaryName("french"))
	suggestions, _ = s4.Lookup("épeler", spell.DictionaryOpts(
		spell.DictionaryName("french"),
	))
	fmt.Println(suggestions)
	// -> [épeler]
}
```

## Credits

Spell makes use of a symmetric delete algorithm and is loosely based on the
[SymSpell](https://github.com/wolfgarbe/SymSpell) implementation.
