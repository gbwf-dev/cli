package ort

import (
	"errors"
	"fmt"
	"io"

	"gbwf/ort/diff3"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/merkletrie"
)

const (
	FastForwardMerge git.MergeStrategy = iota
	FastForwardOnly
	OrtMerge
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
			// Only our file has changed
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
					_, err = dstFile.Write([]byte(content))
					if err != nil {
						return err
					}
					if _, err := w.Add(filename); err != nil {
						return err
					}

				// Our file was deleted
				case merkletrie.Delete:
					err = w.Filesystem.Remove(filename)
					if err != nil {
						return err
					}
					if _, err = w.Remove(filename); err != nil {
						return err
					}
				}
			}

			// Only their file has changed
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
					if _, err = w.Remove(filename); err != nil {
						return err
					}
				}
			}

			// Both changed the file
			if pair.ours != nil && pair.theirs != nil {
				var base, ours, theirs *object.File
				base, ours, err = pair.ours.Files()
				if err != nil {
					return err
				}
				base, theirs, err = pair.theirs.Files()
				if err != nil {
					return err
				}

				// If they made the same changes
				if ours.Blob == theirs.Blob {
					continue // Skip
				}

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
					var baseReader io.ReadCloser
					baseReader, err = base.Blob.Reader()
					if err != nil {
						return err
					}
					defer func() { _ = baseReader.Close() }()

					var oursReader io.ReadCloser
					oursReader, err = ours.Blob.Reader()
					if err != nil {
						return err
					}
					defer func() { _ = oursReader.Close() }()

					_, theirs, err = pair.theirs.Files()
					if err != nil {
						return err
					}

					var theirsReader io.ReadCloser
					theirsReader, err = theirs.Blob.Reader()
					if err != nil {
						return err
					}
					defer func() { _ = theirsReader.Close() }()

					mergeResult, err := diff3.Merge(oursReader, baseReader, theirsReader, true, head.Name().Short(), ref.Name().Short())
					if err != nil {
						return err
					}

					file, err := w.Filesystem.Create(filename)
					if err != nil {
						return err
					}
					defer func() { _ = file.Close() }()

					_, err = io.Copy(file, mergeResult.Result)
					if err != nil {
						return err
					}

					if !mergeResult.Conflicts {
						if _, err = w.Add(filename); err != nil {
							return err
						}
					}

					hasConflicts = hasConflicts || mergeResult.Conflicts

				// Deleted by both
				case ourAction == merkletrie.Delete && theirAction == merkletrie.Delete:
					if err = w.Filesystem.Remove(filename); err != nil {
						return err
					}
					if _, err = w.Remove(filename); err != nil {
						return err
					}

				// Inserted / Modified by us, deleted by them
				case (ourAction == merkletrie.Insert || ourAction == merkletrie.Modify) && theirAction == merkletrie.Delete:
					dstFile, err := w.Filesystem.Create(filename)
					if err != nil {
						return err
					}
					var oursReader io.ReadCloser
					oursReader, err = ours.Reader()
					if err != nil {
						return err
					}
					_, err = io.Copy(dstFile, oursReader)
					if err != nil {
						return err
					}
					if _, err := w.Add(filename); err != nil {
						return err
					}

				// Inserted / Modified by them, deleted by us
				case (theirAction == merkletrie.Insert || theirAction == merkletrie.Modify) && ourAction == merkletrie.Delete:
					dstFile, err := w.Filesystem.Create(filename)
					if err != nil {
						return err
					}
					var theirsReader io.ReadCloser
					theirsReader, err = theirs.Reader()
					if err != nil {
						return err
					}
					_, err = io.Copy(dstFile, theirsReader)
					if err != nil {
						return err
					}
					if _, err := w.Add(filename); err != nil {
						return err
					}
				}
			}
		}

		if hasConflicts {
			return ErrMergeConflict
		}
		_, err = w.Commit(
			"Merge",
			&git.CommitOptions{
				Author:    &oursCommit.Author,
				Committer: &oursCommit.Committer,
				Parents:   []plumbing.Hash{oursCommit.Hash, theirsCommit.Hash},
			},
		)

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
