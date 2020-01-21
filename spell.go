// Copyright (c) 2020 Hayden Eskriett. All rights reserved.
// Use of this source code is governed by a MIT license that can be found in the
// LICENSE file.

// Package spell provides fast spelling correction and string segmentation
package spell

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"github.com/eskriett/strmet"
	"github.com/mitchellh/mapstructure"
	"github.com/tidwall/gjson"
)

type suggestionLevel int
type deletes map[uint32]bool

// Suggestion Levels used during Lookup.
const (
	// LevelBest will yield 'best' suggestion
	LevelBest suggestionLevel = iota

	// LevelClosest will yield closest suggestions
	LevelClosest

	// LevelAll will yield all suggestions
	LevelAll
)

const (
	defaultDict         = "default"
	defaultEditDistance = 2
	defaultPrefixLength = 7
)

// Spell provides access to functions for spelling correction
type Spell struct {
	// The max number of deletes that will be performed to each word in the
	// dictionary
	MaxEditDistance uint32

	// The prefix length that will be examined
	PrefixLength uint32

	cumulativeFreq    uint64
	dictionaryDeletes *dictionaryDeletes
	longestWord       uint32
	library           *library
}

// WordData stores metadata about a word.
type WordData map[string]interface{}

// Entry represents a word in the dictionary
type Entry struct {
	Frequency uint64 `json:",omitempty"`
	Word      string
	WordData  WordData `json:",omitempty"`
}

// New creates a new spell instance
func New() *Spell {
	s := new(Spell)
	s.cumulativeFreq = 0
	s.dictionaryDeletes = newDictionaryDeletes()
	s.longestWord = 0
	s.MaxEditDistance = defaultEditDistance
	s.PrefixLength = defaultPrefixLength
	s.library = newLibrary()
	return s
}

// Load a dictionary from disk from filename. Returns a new Spell instance on
// success, or will return an error if there's a problem reading the file.
func Load(filename string) (*Spell, error) {
	s := New()

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadAll(gz)
	if err != nil {
		return nil, err
	}

	err = f.Close()
	if err != nil {
		return nil, err
	}

	err = gz.Close()
	if err != nil {
		return nil, err
	}

	// Load the words
	gj := gjson.ParseBytes(data)
	gj.Get("words").ForEach(func(dictionary, entries gjson.Result) bool {
		entries.ForEach(func(word, definition gjson.Result) bool {
			e := Entry{}
			if err := mapstructure.Decode(definition.Value(), &e); err != nil {
				log.Fatal(err)
			}

			if _, err := s.AddEntry(e); err != nil {
				log.Fatal(err)
			}
			return true
		})
		return true
	})

	if gj.Get("options.editDistance").Exists() {
		s.MaxEditDistance = uint32(gj.Get("options.editDistance").Int())
	}

	if gj.Get("options.prefixLength").Exists() {
		s.PrefixLength = uint32(gj.Get("options.prefixLength").Int())
	}

	return s, nil
}

type dictOptions struct {
	name string
}

// DictionaryOption is a function that controls the dictionary being used.
// An error will be returned if a dictionary option is invalid
type DictionaryOption func(*dictOptions) error

func (s *Spell) defaultDictOptions() *dictOptions {
	return &dictOptions{
		name: defaultDict,
	}
}

// DictionaryName defines the name of the dictionary that should be used when
// storing, deleting, looking up words, etc. If not set, the default dictionary
// will be used
func DictionaryName(name string) DictionaryOption {
	return func(opts *dictOptions) error {
		opts.name = name
		return nil
	}
}

// AddEntry adds an entry to the dictionary. If the word already exists its data
// will be overwritten. Returns true if a new word was added, false otherwise.
// Will return an error if there was a problem adding a word
func (s *Spell) AddEntry(de Entry, opts ...DictionaryOption) (bool, error) {
	dictOptions := s.defaultDictOptions()

	for _, opt := range opts {
		if err := opt(dictOptions); err != nil {
			return false, err
		}
	}

	word := de.Word

	atomic.AddUint64(&s.cumulativeFreq, de.Frequency)

	// If the word already exists, just update its result - we don't need to
	// recalculate the deletes as these should never change
	if _, exists := s.library.load(dictOptions.name, word); exists {
		atomic.AddUint64(&s.cumulativeFreq, ^(de.Frequency - 1))
		s.library.store(dictOptions.name, word, de)
		return false, nil
	}

	s.library.store(dictOptions.name, word, de)

	// Keep track of the longest word in the dictionary
	wordLength := uint32(len([]rune(word)))
	if wordLength > atomic.LoadUint32(&s.longestWord) {
		atomic.StoreUint32(&s.longestWord, wordLength)
	}

	// Get the deletes for the word. For each delete, hash it and associate the
	// word with it
	deletes := s.getDeletes(word)
	if len(deletes) > 0 {
		wordRunes := []rune(word)

		de := deleteEntry{
			len:   len(wordRunes),
			runes: wordRunes,
			str:   word,
		}
		for deleteHash := range deletes {
			s.dictionaryDeletes.add(dictOptions.name, deleteHash, &de)
		}
	}

	return true, nil
}

// GetEntry returns the Entry for word. If a word does not exist, nil will
// be returned
func (s *Spell) GetEntry(word string, opts ...DictionaryOption) (*Entry, error) {
	dictOpts := s.defaultDictOptions()

	for _, opt := range opts {
		if err := opt(dictOpts); err != nil {
			return nil, err
		}
	}

	if entry, exists := s.library.load(dictOpts.name, word); exists {
		return &entry, nil
	}
	return nil, nil
}

// GetLongestWord returns the length of the longest word in the dictionary
func (s *Spell) GetLongestWord() uint32 {
	return atomic.LoadUint32(&s.longestWord)
}

// RemoveEntry removes a entry from the dictionary. Returns true if the entry
// was removed, false otherwise
func (s *Spell) RemoveEntry(word string, opts ...DictionaryOption) (bool, error) {
	dictOpts := s.defaultDictOptions()

	for _, opt := range opts {
		if err := opt(dictOpts); err != nil {
			return false, err
		}
	}

	return s.library.remove(dictOpts.name, word), nil
}

// Save a representation of spell to disk at filename
func (s *Spell) Save(filename string) error {
	jsonStr, _ := json.Marshal(map[string]interface{}{
		"options": map[string]interface{}{
			"editDistance": s.MaxEditDistance,
			"prefixLength": s.PrefixLength,
		},
		"words": s.library.dictionaries,
	})

	f, err := os.Create(filename)
	if err != nil {
		return err
	}

	w := gzip.NewWriter(f)
	_, err = w.Write(jsonStr)
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	return nil
}

// Suggestion is used to represent a suggested word from a lookup.
type Suggestion struct {
	// The distance between this suggestion and the input word
	Distance int
	Entry
}

// SuggestionList is a slice of Suggestion
type SuggestionList []Suggestion

// GetWords returns a string slice of words for the suggestions
func (s SuggestionList) GetWords() []string {
	words := make([]string, 0, len(s))
	for _, v := range s {
		words = append(words, v.Entry.Word)
	}
	return words
}

// String returns a string representation of the SuggestionList.
func (s SuggestionList) String() string {
	return "[" + strings.Join(s.GetWords(), ", ") + "]"
}

type lookupParams struct {
	dictOpts         *dictOptions
	distanceFunction func([]rune, []rune, int) int
	editDistance     uint32
	prefixLength     uint32
	sortFunc         func(SuggestionList)
	suggestionLevel  suggestionLevel
}

func (s *Spell) defaultLookupParams() *lookupParams {
	return &lookupParams{
		dictOpts:         s.defaultDictOptions(),
		distanceFunction: strmet.DamerauLevenshteinRunes,
		editDistance:     s.MaxEditDistance,
		prefixLength:     s.PrefixLength,
		sortFunc: func(results SuggestionList) {
			sort.Slice(results, func(i, j int) bool {
				s1 := results[i]
				s2 := results[j]

				if s1.Distance < s2.Distance {
					return true
				} else if s1.Distance == s2.Distance {
					return s1.Frequency > s2.Frequency
				}

				return false
			})
		},
		suggestionLevel: LevelBest,
	}
}

// LookupOption is a function that controls how a Lookup is performed. An error
// will be returned if the LookupOption is invalid.
type LookupOption func(*lookupParams) error

// DictionaryOpts accepts multiple DictionaryOption and controls what
// dictionary should be used during lookup
func DictionaryOpts(opts ...DictionaryOption) LookupOption {
	return func(params *lookupParams) error {
		for _, opt := range opts {
			if err := opt(params.dictOpts); err != nil {
				return err
			}
		}
		return nil
	}
}

// DistanceFunc accepts a function, f(str1, str2, maxDist), which calculates the
// distance between two strings. It should return -1 if the distance between the
// strings is greater than maxDist.
func DistanceFunc(df func([]rune, []rune, int) int) LookupOption {
	return func(lp *lookupParams) error {
		lp.distanceFunction = df
		return nil
	}
}

// EditDistance allows the max edit distance to be set for the Lookup. Reducing
// the edit distance will improve lookup performance.
func EditDistance(dist uint32) LookupOption {
	return func(lp *lookupParams) error {
		lp.editDistance = dist
		return nil
	}
}

// SortFunc allows the sorting of the SuggestionList to be configured. By
// default, suggestions will be sorted by their edit distance, then their
// frequency.
func SortFunc(sf func(SuggestionList)) LookupOption {
	return func(lp *lookupParams) error {
		lp.sortFunc = sf
		return nil
	}
}

// SuggestionLevel defines how many results are returned for the lookup. See the
// package constants for the levels available.
func SuggestionLevel(level suggestionLevel) LookupOption {
	return func(lp *lookupParams) error {
		lp.suggestionLevel = level
		return nil
	}
}

// PrefixLength defines how much of the input word should be used for the
// lookup.
func PrefixLength(prefixLength uint32) LookupOption {
	return func(lp *lookupParams) error {
		if prefixLength < 1 {
			return errors.New("prefix length must be greater than 0")
		}
		lp.prefixLength = prefixLength
		return nil
	}
}

func (s *Spell) newDictSuggestion(input string, dist int, dp *dictOptions) Suggestion {
	entry, _ := s.library.load(dp.name, input)

	return Suggestion{
		Distance: dist,
		Entry:    entry,
	}
}

// Lookup takes an input and returns suggestions from the dictionary for that
// word. By default it will return the best suggestion for the word if it
// exists.
//
// Accepts zero or more LookupOption that can be used to configure how lookup
// occurs.
func (s *Spell) Lookup(input string, opts ...LookupOption) (SuggestionList, error) {
	lookupParams := s.defaultLookupParams()

	for _, opt := range opts {
		if err := opt(lookupParams); err != nil {
			return nil, err
		}
	}

	results := SuggestionList{}
	dict := lookupParams.dictOpts.name

	// Check for an exact match
	if _, exists := s.library.load(dict, input); exists {
		results = append(results, s.newDictSuggestion(input, 0, lookupParams.dictOpts))

		if lookupParams.suggestionLevel != LevelAll {
			return results, nil
		}
	}

	editDistance := int(lookupParams.editDistance)

	// If edit distance is 0, just check if input is in the dictionary
	if editDistance == 0 {
		return results, nil
	}

	inputRunes := []rune(input)
	inputLen := len(inputRunes)
	prefixLength := int(lookupParams.prefixLength)

	// Keep track of the deletes we've already considered
	consideredDeletes := make(map[string]bool)

	// Keep track of the suggestions we've already considered
	consideredSuggestions := make(map[string]bool)
	consideredSuggestions[input] = true

	// Keep a list of words we want to try
	var candidates []string

	// Restrict the length of the input we'll examine
	inputPrefixLen := min(inputLen, prefixLength)
	candidates = append(candidates, substring(input, 0, inputPrefixLen))

	for i := 0; i < len(candidates); i++ {
		candidate := candidates[i]
		candidateLen := len([]rune(candidate))
		lengthDiff := inputPrefixLen - candidateLen

		// If the difference between the prefixed input and candidate is larger
		// than the max edit distance then skip the candidate
		if lengthDiff > editDistance {
			if lookupParams.suggestionLevel == LevelAll {
				continue
			}
			break
		}

		candidateHash := getStringHash(candidate)
		if suggestions, exists := s.dictionaryDeletes.load(dict, candidateHash); exists {
			for _, suggestion := range suggestions {
				suggestionLen := suggestion.len

				// Ignore the suggestion if it equals the input
				if suggestion.str == input {
					continue
				}

				// Skip the suggestion if:
				// * Its length difference to the input is greater than the max
				//   edit distance
				// * Its length is less than the current candidate (occurs in
				//   the case of hash collision)
				// * Its length is the same as the candidate and is *not* the
				//   candidate (in the case of a hash collision)
				if abs(suggestionLen-inputLen) > editDistance ||
					suggestionLen < candidateLen ||
					(suggestionLen == candidateLen && suggestion.str != candidate) {
					continue
				}

				// Skip suggestion if its edit distance is too far from input
				suggPrefixLen := min(suggestionLen, prefixLength)
				if suggPrefixLen > inputPrefixLen &&
					(suggPrefixLen-candidateLen) > editDistance {
					continue
				}

				var dist int

				// If the candidate is an empty string and maps to a bin with
				// suggestions (i.e. hash collision), ignore the suggestion if
				// its edit distance with the input is greater than max edit
				// distance
				if candidateLen == 0 {
					dist = max(inputLen, suggestionLen)
					if dist > editDistance ||
						!addKey(consideredSuggestions, suggestion.str) {
						continue
					}
				} else if suggestionLen == 1 {

					// If the length of the suggestion is 1, determine if the
					// input contains the suggestion. If it does than the edit
					// distance is input - 1, otherwise it's the length of the
					// input
					if strings.Contains(input, suggestion.str) {
						dist = inputLen - 1
					} else {
						dist = inputLen
					}

					if dist > editDistance ||
						!addKey(consideredSuggestions, suggestion.str) {
						continue
					}
				} else {
					if !addKey(consideredSuggestions, suggestion.str) {
						continue
					}
					if dist = lookupParams.distanceFunction(inputRunes, suggestion.runes, editDistance); dist < 1 {
						continue
					}
				}

				// Determine whether or not this suggestion should be added to
				// the results and if so, how.
				if dist <= editDistance {
					if len(results) > 0 {
						switch lookupParams.suggestionLevel {
						case LevelClosest:
							if dist < editDistance {
								results = SuggestionList{}
							}
						case LevelBest:
							entry, _ := s.library.load(lookupParams.dictOpts.name, suggestion.str)

							curFreq := entry.Frequency
							closestFreq := results[0].Frequency

							if dist < editDistance || curFreq > closestFreq {
								editDistance = dist
								results[0] = s.newDictSuggestion(suggestion.str, dist, lookupParams.dictOpts)
							}
							continue
						}
					}

					if lookupParams.suggestionLevel != LevelAll {
						editDistance = dist
					}

					results = append(results,
						s.newDictSuggestion(suggestion.str, dist, lookupParams.dictOpts))
				}

			}
		}

		// Add additional candidates
		if lengthDiff < editDistance && candidateLen <= prefixLength {

			if lookupParams.suggestionLevel != LevelAll && lengthDiff > editDistance {
				continue
			}

			for i := 0; i < candidateLen; i++ {
				deleteWord := removeChar(candidate, i)

				if addKey(consideredDeletes, deleteWord) {
					candidates = append(candidates, deleteWord)
				}
			}
		}
	}

	// Order the results
	lookupParams.sortFunc(results)

	return results, nil
}

type segmentParams struct {
	lookupOptions []LookupOption
}

func (s *Spell) defaultSegmentParams() *segmentParams {
	return &segmentParams{
		lookupOptions: []LookupOption{
			SuggestionLevel(LevelBest),
		},
	}
}

// SegmentOption is a function that controls how a Segment is performed. An
// error will be returned if the SegmentOption is invalid.
type SegmentOption func(*segmentParams) error

// SegmentLookupOpts allows the Lookup() options for the current segmentation to
// be configured
func SegmentLookupOpts(opt ...LookupOption) SegmentOption {
	return func(sp *segmentParams) error {
		sp.lookupOptions = opt
		return nil
	}
}

// Segment contains details about an individual segment
type Segment struct {
	Input string
	Entry *Entry
	Word  string
}

// SegmentResult holds the result of a call to Segment()
type SegmentResult struct {
	Distance int
	Segments []Segment
}

// GetWords returns a string slice of words for the segments
func (s SegmentResult) GetWords() []string {
	words := make([]string, 0, len(s.Segments))
	for _, s := range s.Segments {
		words = append(words, s.Word)
	}
	return words
}

// String returns a string representation of the SegmentList.
func (s SegmentResult) String() string {
	return strings.Join(s.GetWords(), " ")
}

// Segment takes an input string which may have word concatenations, and
// attempts to divide it into the most likely set of words by adding spaces at
// the most appropriate positions.
//
// Accepts zero or more SegmentOption that can be used to configure how
// segmentation occurs
func (s *Spell) Segment(input string, opts ...SegmentOption) (*SegmentResult, error) {
	segmentParams := s.defaultSegmentParams()

	for _, opt := range opts {
		if err := opt(segmentParams); err != nil {
			return nil, err
		}
	}

	longestWord := int(atomic.LoadUint32(&s.longestWord))
	if longestWord == 0 {
		return nil, errors.New("longest word in dictionary has zero length")
	}

	cumulativeFreq := float64(atomic.LoadUint64(&s.cumulativeFreq))
	if cumulativeFreq == 0 {
		return nil, errors.New("cumulative frequency is zero")
	}

	inputLen := len([]rune(input))

	arraySize := min(inputLen, longestWord)
	circularIdx := -1

	type composition struct {
		segmentedString string
		correctedString string
		distanceSum     int
		probability     float64
	}
	compositions := make([]composition, arraySize)

	for i := 0; i < inputLen; i++ {

		jMax := min(inputLen-i, longestWord)

		for j := 1; j <= jMax; j++ {
			part := substring(input, i, i+j)

			separatorLength := 0
			topEd := 0
			topProbabilityLog := 0.0
			topResult := ""

			if unicode.Is(unicode.White_Space, rune(part[0])) {
				part = substring(input, i+1, i+j)
			} else {
				separatorLength = 1
			}

			topEd += len([]rune(part))
			part = strings.Replace(part, " ", "", -1)
			topEd -= len([]rune(part))

			suggestions, err := s.Lookup(part, segmentParams.lookupOptions...)
			if err != nil {
				return nil, err
			}

			if len(suggestions) > 0 {
				topResult = suggestions[0].Entry.Word
				topEd += suggestions[0].Distance

				freq := suggestions[0].Frequency
				topProbabilityLog = math.Log10(float64(freq) / cumulativeFreq)
			} else {
				// Unknown word
				topResult = part
				topEd += len([]rune(part))
				topProbabilityLog = math.Log10(10.0 / (cumulativeFreq *
					math.Pow(10.0, float64(len([]rune(part))))))
			}

			destinationIdx := (j + circularIdx) % arraySize

			if i == 0 {
				compositions[destinationIdx] = composition{
					segmentedString: part,
					correctedString: topResult,
					distanceSum:     topEd,
					probability:     topProbabilityLog,
				}
			} else if j == longestWord ||
				((compositions[circularIdx].distanceSum+topEd ==
					compositions[destinationIdx].distanceSum ||
					compositions[circularIdx].distanceSum+separatorLength+topEd ==
						compositions[destinationIdx].distanceSum) &&
					compositions[destinationIdx].probability < compositions[circularIdx].probability+topProbabilityLog) ||
				compositions[circularIdx].distanceSum+separatorLength+topEd <
					compositions[destinationIdx].distanceSum {
				compositions[destinationIdx] = composition{
					segmentedString: compositions[circularIdx].segmentedString + " " + part,
					correctedString: compositions[circularIdx].correctedString + " " + topResult,
					distanceSum:     compositions[circularIdx].distanceSum + separatorLength + topEd,
					probability:     compositions[circularIdx].probability + topProbabilityLog,
				}
			}
		}

		circularIdx++
		if circularIdx == arraySize {
			circularIdx = 0
		}
	}

	segmentedString := compositions[circularIdx].segmentedString
	correctedString := compositions[circularIdx].correctedString
	segmentedWords := strings.Split(segmentedString, " ")
	correctedWords := strings.Split(correctedString, " ")
	segments := make([]Segment, len(correctedWords))

	for i, word := range correctedWords {
		e, err := s.GetEntry(word)
		if err != nil {
			return nil, err
		}

		segments[i] = Segment{
			Input: segmentedWords[i],
			Word:  word,
			Entry: e,
		}
	}

	result := SegmentResult{
		Distance: compositions[circularIdx].distanceSum,
		Segments: segments,
	}

	return &result, nil
}

func (s *Spell) generateDeletes(word string, editDistance uint32, deletes deletes) deletes {
	editDistance++

	if wordLen := len([]rune(word)); wordLen > 1 {
		for i := 0; i < wordLen; i++ {
			deleteWord := removeChar(word, i)
			deleteHash := getStringHash(deleteWord)

			if _, exists := deletes[deleteHash]; !exists {
				deletes[deleteHash] = true

				if editDistance < s.MaxEditDistance {
					s.generateDeletes(deleteWord, editDistance, deletes)
				}
			}

		}
	}

	return deletes
}

func (s *Spell) getDeletes(word string) deletes {
	deletes := deletes{}
	wordLen := len([]rune(word))

	// Restrict the size of the word to the max length of the prefix we'll
	// examine
	if wordLen > int(s.PrefixLength) {
		word = substring(word, 0, int(s.PrefixLength))
	}

	wordHash := getStringHash(word)
	deletes[wordHash] = true

	return s.generateDeletes(word, 0, deletes)
}

type dictionaryDeletes struct {
	sync.RWMutex
	dictionaries map[string]deletesMap
}

type deletesMap map[uint32][]*deleteEntry

type deleteEntry struct {
	len   int
	runes []rune
	str   string
}

func newDictionaryDeletes() *dictionaryDeletes {
	return &dictionaryDeletes{
		dictionaries: make(map[string]deletesMap),
	}
}

func (dd *dictionaryDeletes) load(dict string, key uint32) ([]*deleteEntry, bool) {
	dd.RLock()
	entry, exists := dd.dictionaries[dict][key]
	dd.RUnlock()
	return entry, exists
}

func (dd *dictionaryDeletes) add(dict string, key uint32, entry *deleteEntry) {
	dd.Lock()
	if _, exists := dd.dictionaries[dict]; !exists {
		dd.dictionaries[dict] = make(deletesMap)
	}

	dd.dictionaries[dict][key] = append(dd.dictionaries[dict][key], entry)
	dd.Unlock()
}

// library is a collection of dictionaries
type library struct {
	sync.RWMutex
	dictionaries map[string]dictionary
}

// dictionary is a mapping of a word to its dictionary entry
type dictionary map[string]Entry

// newLibrary creates an empty library of dictionaries
func newLibrary() *library {
	return &library{
		dictionaries: make(map[string]dictionary),
	}
}

// load checks if a word exists in a given dictionary
func (l *library) load(dict, word string) (Entry, bool) {
	l.RLock()
	definition, exists := l.dictionaries[dict][word]
	l.RUnlock()
	return definition, exists
}

// store adds a word to a given dictionary
func (l *library) store(dict, word string, definition Entry) {
	l.Lock()
	if _, exists := l.dictionaries[dict]; !exists {
		l.dictionaries[dict] = make(dictionary)
	}

	l.dictionaries[dict][word] = definition

	l.Unlock()
}

// remove deletes a word from a given dictionary
func (l *library) remove(dict, word string) bool {
	l.Lock()
	defer l.Unlock()

	if _, exists := l.dictionaries[dict][word]; exists {
		delete(l.dictionaries[dict], word)
		return true
	}

	return false
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func addKey(hash map[string]bool, key string) bool {
	if _, exists := hash[key]; exists {
		return false
	}

	hash[key] = true

	return true
}

// FNV-1a hash implementation
func getStringHash(str string) uint32 {
	var h uint32 = 2166136261
	for _, c := range []byte(str) {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func removeChar(str string, index int) string {
	return substring(str, 0, index) + substring(str, index+1, len([]rune(str)))
}

func substring(s string, start int, end int) string {
	if start >= len([]rune(s)) {
		return ""
	}

	startStrIdx := 0
	i := 0

	for j := range s {
		if i == start {
			startStrIdx = j
		}
		if i == end {
			return s[startStrIdx:j]
		}
		i++
	}
	return s[startStrIdx:]
}
