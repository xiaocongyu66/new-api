// Package channel owns channel administration and selection rules. It holds
// use-case and topic files rather than a controller/service/model split, and it
// implements the gateway's selection port instead of being called into directly.
package channel

// ExclusionSet tracks channels already attempted and failed for one request.
//
// Relay retries must not reselect a channel that just failed, otherwise a single
// broken upstream can consume every retry while healthy channels go untried.
type ExclusionSet map[int]bool

// NewExclusionSet builds an exclusion set from channel ids.
//
// Non-positive ids are dropped: they cannot identify a real channel, and keeping
// them would let a malformed retry state silently exclude nothing while looking
// populated.
func NewExclusionSet(channelIDs []int) ExclusionSet {
	if len(channelIDs) == 0 {
		return nil
	}

	excluded := make(ExclusionSet, len(channelIDs))
	for _, id := range channelIDs {
		if id <= 0 {
			continue
		}
		excluded[id] = true
	}
	if len(excluded) == 0 {
		return nil
	}
	return excluded
}

// Excludes reports whether a channel was already tried for this request.
func (e ExclusionSet) Excludes(channelID int) bool {
	if len(e) == 0 {
		return false
	}
	return e[channelID]
}

// With returns a set that additionally excludes channelID.
//
// It copies rather than mutating in place so a retry loop cannot retroactively
// change the exclusion state observed by an in-flight selection.
func (e ExclusionSet) With(channelID int) ExclusionSet {
	if channelID <= 0 {
		return e
	}

	extended := make(ExclusionSet, len(e)+1)
	for id := range e {
		extended[id] = true
	}
	extended[channelID] = true
	return extended
}

// IDs returns the excluded channel ids. The result is intended for logging and
// for handing to the persistence query, so callers must not rely on its order.
func (e ExclusionSet) IDs() []int {
	if len(e) == 0 {
		return nil
	}

	ids := make([]int, 0, len(e))
	for id, excluded := range e {
		if excluded {
			ids = append(ids, id)
		}
	}
	return ids
}
