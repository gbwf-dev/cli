package ort

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/sergi/go-diff/diffmatchpatch"
)

const (
	FastForwardOnly git.MergeStrategy = iota
	FastForwardMerge
	OrtMerge
)

const (
	ConflictOurMarker   = "<<<<<<<"
	ConflictSplitMarker = "======="
	ConflictTheirMarker = ">>>>>>>"
)

var ErrMergeConflict = errors.New("merge conflict")

type MergeOptions struct {
	Strategy               git.MergeStrategy
	OrtMergeStrategyOption git.OrtMergeStrategyOption
}

func Merge(r *git.Repository, ref plumbing.Reference, opts MergeOptions) error {
	head, err := r.Head()
	if err != nil {
		return err
	}
	// Ignore error as not having a shallow list is optional here.
	shallowList, _ := r.Storer.Shallow()
	var earliestShallow *plumbing.Hash
	if len(shallowList) > 0 {
		earliestShallow = &shallowList[0]
	}

	ff, err := isFastForward(r.Storer, head.Hash(), ref.Hash(), earliestShallow)
	if err != nil {
		return err
	}

	switch opts.Strategy {
	case FastForwardOnly:
		if !ff {
			return git.ErrFastForwardMergeNotPossible
		}
		return r.Storer.SetReference(plumbing.NewHashReference(head.Name(), ref.Hash()))

	case FastForwardMerge:
		if ff {
			return r.Storer.SetReference(plumbing.NewHashReference(head.Name(), ref.Hash()))
		}
		fallthrough

	case OrtMerge:
		theirsCommit, err := r.CommitObject(ref.Hash())
		if err != nil {
			return err
		}

		oursCommit, err := r.CommitObject(head.Hash())
		if err != nil {
			return err
		}

		baseCommits, err := oursCommit.MergeBase(theirsCommit)
		if err != nil {
			return err
		}

		if len(baseCommits) < 1 {
			return fmt.Errorf("unable to merge with different history")
		}

		baseTree, err := baseCommits[0].Tree()
		if err != nil {
			return err
		}

		oursTree, err := oursCommit.Tree()
		if err != nil {
			return err
		}
		theirsTree, err := theirsCommit.Tree()
		if err != nil {
			return err
		}

		baseToOurs, err := baseTree.Diff(oursTree)
		if err != nil {
			return err
		}

		baseToTheirs, err := baseTree.Diff(theirsTree)
		if err != nil {
			return err
		}

		changes := make(map[string]struct {
			ours   *object.Change
			theirs *object.Change
		})

		for _, change := range baseToOurs {
			pair := changes[change.To.Name]
			pair.ours = change
			changes[change.To.Name] = pair
		}

		for _, change := range baseToTheirs {
			pair := changes[change.To.Name]
			pair.theirs = change
			changes[change.To.Name] = pair
		}
		delete(changes, "")

		w, err := r.Worktree()
		if err != nil {
			return err
		}

		hasConflicts := false

		for filename, pair := range changes {
			// Our file has changed
			if pair.ours != nil && pair.theirs == nil {
				action, err := pair.ours.Action()
				if err != nil {
					return err
				}

				switch action {
				case merkletrie.Insert, merkletrie.Modify:
					_, to, err := pair.ours.Files()
					if err != nil {
						return err
					}
					content, err := to.Contents()
					if err != nil {
						return err
					}

					dstFile, err := w.Filesystem.Create(filename)
					if err != nil {
						return err
					}
					diffmatchpatch.New()
					_, err = dstFile.Write([]byte(content))
					if err != nil {
						return err
					}
					if _, err := w.Add(filename); err != nil {
						return err
					}

				case merkletrie.Delete:
					err = w.Filesystem.Remove(filename)
					if err != nil {
						return err
					}
					if _, err = w.Add(filename); err != nil {
						return err
					}
				}
			}

			// Their file has changed
			if pair.ours == nil && pair.theirs != nil {
				action, err := pair.theirs.Action()
				if err != nil {
					return err
				}

				switch action {
				case merkletrie.Insert, merkletrie.Modify:
					_, to, err := pair.theirs.Files()
					if err != nil {
						return err
					}
					content, err := to.Contents()
					if err != nil {
						return err
					}

					dstFile, err := w.Filesystem.Create(filename)
					if err != nil {
						return err
					}
					_, err = dstFile.Write([]byte(content))
					if err != nil {
						return err
					}
					if _, err = w.Add(filename); err != nil {
						return err
					}

				case merkletrie.Delete:
					err = w.Filesystem.Remove(filename)
					if err != nil {
						return err
					}
					if _, err = w.Add(filename); err != nil {
						return err
					}
				}
			}

			// Both changed the file
			if pair.ours != nil && pair.theirs != nil {
				// If they made the same changes
				if pair.ours.To.TreeEntry.Hash == pair.theirs.From.TreeEntry.Hash {
					continue
				}

				hasConflicts = true

				ourAction, err := pair.ours.Action()
				if err != nil {
					return err
				}

				theirAction, err := pair.theirs.Action()
				if err != nil {
					return err
				}

				switch {

				// Added or Modified by both
				case ourAction == merkletrie.Modify && theirAction == merkletrie.Modify,
					ourAction == merkletrie.Insert && theirAction == merkletrie.Insert:
					dmp := diffmatchpatch.New()
					var baseBlob *object.Blob
					baseBlob, err = r.BlobObject(pair.ours.From.TreeEntry.Hash)
					if err != nil {
						return err
					}

					baseReader, err := baseBlob.Reader()
					if err != nil {
						return err
					}
					defer func() { _ = baseReader.Close() }()

					baseContent, err := io.ReadAll(baseReader)
					if err != nil {
						return err
					}

					var ourBlob *object.Blob
					ourBlob, err = r.BlobObject(pair.ours.To.TreeEntry.Hash)
					if err != nil {
						return err
					}

					ourReader, err := ourBlob.Reader()
					if err != nil {
						return err
					}
					defer func() { _ = ourReader.Close() }()

					ourContent, err := io.ReadAll(ourReader)
					if err != nil {
						return err
					}

					var theirBlob *object.Blob
					theirBlob, err = r.BlobObject(pair.theirs.To.TreeEntry.Hash)
					if err != nil {
						return err
					}

					theirReader, err := theirBlob.Reader()
					if err != nil {
						return err
					}
					defer func() { _ = theirReader.Close() }()

					theirContent, err := io.ReadAll(theirReader)
					if err != nil {
						return err
					}

					ourDiffs := dmp.DiffMain(string(baseContent), string(ourContent), false)
					theirDiffs := dmp.DiffMain(string(baseContent), string(theirContent), false)

					merged := new(bytes.Buffer)
					_, _ = fmt.Fprintf(
						merged,
						"%s\n%s\n%s\n%s\n%s\n",
						ConflictOurMarker,
						dmp.DiffText1(ourDiffs),
						ConflictSplitMarker,
						dmp.DiffText2(theirDiffs),
						ConflictTheirMarker,
					)

					file, err := w.Filesystem.Create(filename)
					if err != nil {
						return err
					}
					defer func() { _ = file.Close() }()

					_, err = file.Write(merged.Bytes())
					if err != nil {
						return err
					}

				case ourAction == merkletrie.Delete && theirAction == merkletrie.Delete:
					if err = w.Filesystem.Remove(filename); err != nil {
						return err
					}
					if _, err = w.Add(filename); err != nil {
						return err
					}
				}

			}
		}

		if hasConflicts {
			return ErrMergeConflict
		}

		return err

	default:
		return git.ErrUnsupportedMergeStrategy
	}
}

func isFastForward(s storer.EncodedObjectStorer, old, newHash plumbing.Hash, earliestShallow *plumbing.Hash) (bool, error) {
	c, err := object.GetCommit(s, newHash)
	if err != nil {
		return false, err
	}

	parentsToIgnore := []plumbing.Hash{}
	if earliestShallow != nil {
		earliestCommit, err := object.GetCommit(s, *earliestShallow)
		if err != nil {
			return false, err
		}

		parentsToIgnore = earliestCommit.ParentHashes
	}

	found := false
	// stop iterating at the earliest shallow commit, ignoring its parents
	// note: when pull depth is smaller than the number of new changes on the remote, this fails due to missing parents.
	//       as far as i can tell, without the commits in-between the shallow pull and the earliest shallow, there's no
	//       real way of telling whether it will be a fast-forward merge.
	iter := object.NewCommitPreorderIter(c, nil, parentsToIgnore)
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Hash != old {
			return nil
		}

		found = true
		return storer.ErrStop
	})
	return found, err
}
