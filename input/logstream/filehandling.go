/***** BEGIN LICENSE BLOCK *****
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this file,
# You can obtain one at http://mozilla.org/MPL/2.0/.
#
# The Initial Developer of the Original Code is the Mozilla Foundation.
# Portions created by the Initial Developer are Copyright (C) 2012
# the Initial Developer. All Rights Reserved.
#
# Contributor(s):
#   Ben Bangert (bbangert@mozilla.com)
#
# ***** END LICENSE BLOCK *****/

package logstream

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var MonthLookup = map[string]int{
	"january":   1,
	"jan":       1,
	"february":  2,
	"feb":       2,
	"march":     3,
	"mar":       3,
	"april":     4,
	"apr":       4,
	"may":       5,
	"june":      6,
	"jun":       6,
	"july":      7,
	"jul":       7,
	"august":    8,
	"aug":       8,
	"september": 9,
	"sep":       9,
	"october":   10,
	"oct":       10,
	"november":  11,
	"nov":       11,
	"december":  12,
	"dec":       12,
}

var DayLookup = map[string]int{
	"monday":    0,
	"mon":       0,
	"tuesday":   1,
	"tue":       1,
	"wednesday": 2,
	"wed":       2,
	"thursday":  3,
	"thu":       3,
	"friday":    4,
	"fri":       4,
	"saturday":  5,
	"sat":       5,
	"sunday":    6,
	"sun":       6,
}

var digitRegex = regexp.MustCompile(`^\d+$`)

// Custom multiple error type that satisfies Go error interface but has
// alternate printing options
type MultipleError []string

func NewMultipleError() *MultipleError {
	me := make(MultipleError, 0)
	return &me
}

func (m MultipleError) Error() string {
	return strings.Join(m, " :: ")
}

func (m *MultipleError) AddMessage(s string) {
	*m = append(*m, s)
}

func (m MultipleError) IsError() bool {
	return len(m) > 0
}

type Logfile struct {
	FileName string
	// The matched portions of the filename and their translated integer value
	MatchParts map[string]int
	// The raw string matches from the filename
	StringMatchParts map[string]string
}

func (l *Logfile) PopulateMatchParts(subexpNames, matches []string, translation SubmatchTranslationMap) error {
	var score int
	var ok bool
	if l.MatchParts == nil {
		l.MatchParts = make(map[string]int)
	}
	if l.StringMatchParts == nil {
		l.StringMatchParts = make(map[string]string)
	}
	for i, name := range subexpNames {
		matchValue := matches[i]
		// Store the raw string
		l.StringMatchParts[name] = matchValue

		lowerValue := strings.ToLower(matchValue)
		score = -1
		if name == "" {
			continue
		}
		if name == "MonthName" {
			if score, ok = MonthLookup[lowerValue]; !ok {
				return errors.New("Unable to locate month name: " + matchValue)
			}
		} else if name == "DayName" {
			if score, ok = DayLookup[lowerValue]; !ok {
				return errors.New("Unable to locate day name : " + matchValue)
			}
		} else if submap, ok := translation[name]; ok {
			if score, ok = submap[matchValue]; !ok {
				return errors.New("Unable to locate value: (" + matchValue + ") in translation map: " + name)
			}
		} else if digitRegex.MatchString(matchValue) {
			score, _ = strconv.Atoi(matchValue)
		}
		l.MatchParts[name] = score
	}
	return nil
}

type Logfiles []*Logfile

// Implement two of the sort.Interface methods needed
func (l Logfiles) Len() int      { return len(l) }
func (l Logfiles) Swap(i, j int) { l[i], l[j] = l[j], l[i] }

// Locate the index of a given filename if its present in the logfiles for this stream
// Returns -1 if the filename is not present
func (l Logfiles) IndexOf(s string) int {
	for i := 0; i < len(l); i++ {
		if l[i].FileName == s {
			return i
		}
	}
	return -1
}

// Provided the fileMatch regexp and translation map, populate all the Logfile
// matchparts for use in sorting.
func (l Logfiles) PopulateMatchParts(fileMatch *regexp.Regexp, translation SubmatchTranslationMap) error {
	errorlist := NewMultipleError()
	subexpNames := fileMatch.SubexpNames()
	for _, logfile := range l {
		matches := fileMatch.FindStringSubmatch(logfile.FileName)
		if err := logfile.PopulateMatchParts(subexpNames, matches, translation); err != nil {
			errorlist.AddMessage(err.Error())
		}
	}
	if errorlist.IsError() {
		return errorlist
	}
	return nil
}

// ByPriority implements the final method of the sort.Interface so that the embedded
// LogfileMatches may be sorted by the priority of their matches parts
type ByPriority struct {
	Logfiles
	Priority []string
}

// Determine based on priority if which of the two is 'less' than the other
func (b ByPriority) Less(i, j int) bool {
	var convert func(bool) bool
	first := b.Logfiles[i]
	second := b.Logfiles[j]
	for _, part := range b.Priority {
		convert = func(a bool) bool { return a }
		if "^" == part[:1] {
			part = part[1:]
			convert = func(a bool) bool { return !a }
		}
		if first.MatchParts[part] < second.MatchParts[part] {
			return convert(true)
		} else if first.MatchParts[part] > second.MatchParts[part] {
			return convert(false)
		}
	}
	// If we get here, it means all the parts are exactly equal, consider
	// the first not less than the second
	return false
}

// Scans a directory recursively filtering out files that match the fileMatch regexp
func ScanDirectoryForLogfiles(directoryPath string, fileMatch *regexp.Regexp) Logfiles {
	files := make(Logfiles, 0)
	filepath.Walk(directoryPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if fileMatch.MatchString(path) {
			files = append(files, &Logfile{FileName: path})
		}
		return nil
	})
	return files
}

type MultipleLogstreamFileList map[string]Logfiles

// Filter a single Logfiles into a MultipleLogstreamFileList keyed by the
// differentiator.
func FilterMultipleStreamFiles(files Logfiles, differentiator []string) MultipleLogstreamFileList {
	mfs := make(MultipleLogstreamFileList)
	for _, logfile := range files {
		name := ResolveDifferentiatedName(logfile, differentiator)
		_, ok := mfs[name]
		if !ok {
			mfs[name] = make(Logfiles, 0)
		}
		mfs[name] = append(mfs[name], logfile)
	}
	return mfs
}

// Resolve a differentiated name from a Logfile and return it.
func ResolveDifferentiatedName(l *Logfile, differentiator []string) (name string) {
	for _, d := range differentiator {
		if value, ok := l.StringMatchParts[d]; ok {
			name += value
		} else {
			name += d
		}
	}
	return
}

// Match translation map for a matched section that maps the string value to the integer to
// sort on.
type MatchTranslationMap map[string]int

// SubmatchTranslationMap holds a map of MatchTranslationMap's for every submatch that
// should be translated to an int.
type SubmatchTranslationMap map[string]MatchTranslationMap

// Sort pattern is a construct used to return an ordered list of filenames based
// on the supplied sorting criteria
//
// Example:
//
//     Assume that all logfiles are named '2013/August/08/xyz-11.log'. First a
// FileMatch pattern is needed to recognize these parts, which would look like:
//
//     (?P<Year>\d{4})/(?P<MonthName>\w+)/(?P<Day>\d+)/\w+-(?P<Seq>\d+).log
//
//     The above pattern will match the logfiles and break them down to 4 key parts
// used to sort the list. The Year, MonthName (which will be converted to ints), Day,
// and Seq. No Translation mapping is needed in this case, as standard English month
// and day names are translated to integers. The priority for sorting should be done
// with Year first, then MonthName, Day, and finally Seq, so the Priority would be
//
//     ["Year", "MonthName", "Day", "^Seq"]
//
// Note that "Seq" had "^" prefixed to it. This indicates that it should be sorted in
// descending order rather than the default of ascending order because in our case the
// sequence of the log number indicates how many removed from *current* it is, while the
// higher the year, month, day indicates how close it is to *current*.
type SortPattern struct {
	// Regular expression for the files to match and parts of the filename to use for
	// sorting. All parts to be sorted on must be captured and named. Special handling is
	// provided for parts with names: MonthName, DayName.
	// These names will be translated from short/long month/day names to the appropriate
	// integer value.
	FileMatch string
	// Translation is used for custom ordering lookups where a custom value needs to be
	// translated to a value for sorting. ie. a different tool using weekdays with values
	// causing the wrong day of the week to be parsed first
	Translation SubmatchTranslationMap
	// Priority list which should be provided to determine the most important parts of
	// the matches to sort on. Most important captured name should be first. These will
	// be sorted in ascending order representing *oldest first*. If this portions value
	// increasing means its *older*, then it should be sorted in descending order by
	// adding a ^ to the beginning.
	Priority []string
	// Differentiators are used on portions of the file match to indicate unique
	// non-changing portions that combined will yield an identifier for this 'logfile'
	// If the name is not a subregex name, its raw value will be used to identify
	// the log stream.
	Differentiator []string
}
