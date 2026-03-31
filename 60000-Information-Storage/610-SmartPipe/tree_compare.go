package main

// TreeDiff represents the delta between two fully-resolved BranchTrees.
// Because our jeBNF BranchTree contains the pre-calculated Git SHA for every
// file (the true logical content identity), branch comparison is a strictly
// in-memory O(N+M) operation requiring zero network calls or disk reads,
// completely independent of how files are physically packed in the CAS.
type TreeDiff struct {
	BaseBranch   string
	TargetBranch string

	Added    []BranchEntry
	Modified []ModifiedEntry
	Deleted  []BranchEntry
	Renamed  []RenamedEntry // (Optional: requires heuristic matching)

	AddedBytes   int64
	DeletedBytes int64
}

// ModifiedEntry tracks a file that exists in both branches but with different SHAs (content).
type ModifiedEntry struct {
	Path       string
	BaseSHA    string
	TargetSHA  string
	BaseSize   int64
	TargetSize int64
}

// RenamedEntry tracks a file that moved paths but retained the exact same SHA (content).
type RenamedEntry struct {
	OldPath string
	NewPath string
	SHA     string
	Size    int64
}

// CompareBranchTrees computes the delta between a base and target branch tree in sub-millisecond time.
func CompareBranchTrees(base, target BranchTree) TreeDiff {
	diff := TreeDiff{
		BaseBranch:   base.Branch,
		TargetBranch: target.Branch,
		Added:        make([]BranchEntry, 0),
		Modified:     make([]ModifiedEntry, 0),
		Deleted:      make([]BranchEntry, 0),
		Renamed:      make([]RenamedEntry, 0),
	}

	// 1. Build a fast lookup map for the Base branch: Path -> BranchEntry
	baseMap := make(map[string]BranchEntry, len(base.Entries))
	for _, entry := range base.Entries {
		baseMap[entry.Path] = entry
	}

	// 2. Iterate the Target branch to find Additions and Modifications
	// Also, remove matched items from the baseMap so whatever remains is a Deletion.
	for _, targetEntry := range target.Entries {
		if baseEntry, exists := baseMap[targetEntry.Path]; exists {
			// Path exists in both. Compare logical contents via SHA (O(1) string comparison!)
			// This works even if one file is packed and the other is a standalone CAS blob.
			if baseEntry.SHA != targetEntry.SHA {
				diff.Modified = append(diff.Modified, ModifiedEntry{
					Path:       targetEntry.Path,
					BaseSHA:    baseEntry.SHA,
					TargetSHA:  targetEntry.SHA,
					BaseSize:   baseEntry.Size,
					TargetSize: targetEntry.Size,
				})
			}
			// Remove from base map so we don't mark it deleted
			delete(baseMap, targetEntry.Path)
		} else {
			// Path exists in Target, but not Base -> Added
			diff.Added = append(diff.Added, targetEntry)
			diff.AddedBytes += targetEntry.Size
		}
	}

	// 3. Whatever is left in the Base map was deleted in Target
	for _, baseEntry := range baseMap {
		diff.Deleted = append(diff.Deleted, baseEntry)
		diff.DeletedBytes += baseEntry.Size
	}

	// 4. (Optional) Detect Renames: An Added and Deleted file with the exact same CasKey
	diff.detectRenames()

	return diff
}

// detectRenames matches O(1) content identities across the Added and Deleted lists.
func (d *TreeDiff) detectRenames() {
	// Build lookup of added files by their content (SHA)
	addedByHash := make(map[string]BranchEntry)
	for _, added := range d.Added {
		addedByHash[added.SHA] = added
	}

	var remainingDeleted []BranchEntry
	for _, deleted := range d.Deleted {
		if addedMatch, exists := addedByHash[deleted.SHA]; exists {
			// Content is identical, but path changed! It's a rename.
			d.Renamed = append(d.Renamed, RenamedEntry{
				OldPath: deleted.Path,
				NewPath: addedMatch.Path,
				SHA:     deleted.SHA,
				Size:    deleted.Size,
			})
			// Preempt it from being matched twice
			delete(addedByHash, deleted.SHA)
		} else {
			remainingDeleted = append(remainingDeleted, deleted)
		}
	}

	// Rebuild Additions list to remove the ones we just classified as Renames
	var remainingAdded []BranchEntry
	for _, added := range addedByHash {
		remainingAdded = append(remainingAdded, added)
	}

	d.Added = remainingAdded
	d.Deleted = remainingDeleted
}
