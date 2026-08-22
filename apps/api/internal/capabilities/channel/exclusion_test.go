package channel

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewExclusionSetDropsInvalidIDs asserts only usable channel ids enter the
// set. A zero or negative id cannot identify a channel, and keeping one would
// produce a set that looks populated while excluding nothing real.
func TestNewExclusionSetDropsInvalidIDs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    []int
		expected []int
	}{
		{name: "nil input", input: nil, expected: nil},
		{name: "empty input", input: []int{}, expected: nil},
		{name: "valid ids", input: []int{3, 7}, expected: []int{3, 7}},
		{name: "zero dropped", input: []int{0, 5}, expected: []int{5}},
		{name: "negative dropped", input: []int{-2, 5}, expected: []int{5}},
		{name: "all invalid collapses to nil", input: []int{0, -1}, expected: nil},
		{name: "duplicates collapse", input: []int{4, 4, 4}, expected: []int{4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := NewExclusionSet(tc.input)

			ids := set.IDs()
			sort.Ints(ids)
			assert.Equal(t, tc.expected, ids)
		})
	}
}

// TestExclusionSetExcludes asserts membership drives retry decisions, and that an
// empty set excludes nothing so a first attempt is never blocked.
func TestExclusionSetExcludes(t *testing.T) {
	set := NewExclusionSet([]int{11, 12})

	assert.True(t, set.Excludes(11))
	assert.True(t, set.Excludes(12))
	assert.False(t, set.Excludes(13))

	var empty ExclusionSet
	assert.False(t, empty.Excludes(11), "an empty set must not block the first attempt")
}

// TestExclusionSetWithDoesNotMutateReceiver asserts extension copies the set.
// A retry loop must not retroactively alter the exclusions an in-flight selection
// already observed.
func TestExclusionSetWithDoesNotMutateReceiver(t *testing.T) {
	original := NewExclusionSet([]int{21})

	extended := original.With(22)

	assert.True(t, extended.Excludes(21))
	assert.True(t, extended.Excludes(22))
	assert.False(t, original.Excludes(22), "receiver must be unchanged")
	assert.Len(t, original.IDs(), 1)
}

// TestExclusionSetWithIgnoresInvalidID asserts an unusable id is not recorded.
func TestExclusionSetWithIgnoresInvalidID(t *testing.T) {
	original := NewExclusionSet([]int{31})

	assert.False(t, original.With(0).Excludes(0))
	assert.False(t, original.With(-5).Excludes(-5))
	assert.Len(t, original.With(0).IDs(), 1)
}

// TestExclusionSetWithBuildsFromNilReceiver asserts the first failed channel can
// be recorded without pre-allocating a set.
func TestExclusionSetWithBuildsFromNilReceiver(t *testing.T) {
	var set ExclusionSet

	extended := set.With(41)

	assert.True(t, extended.Excludes(41))
	assert.Equal(t, []int{41}, extended.IDs())
}
