package ort

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gbwf/ort/diff3"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/index"
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
	Progress               io.Writer
}

func Merge(r *git.Repository, ref plumbing.Reference, opts MergeOptions) error {
	head, err := r.Head()
	if err != nil {
		return err
	}

	theirsCommit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return err
	}

	oursCommit, err := r.CommitObject(head.Hash())
	if err != nil {
		return err
	}

	var patch *object.Patch
	patch, err = oursCommit.Patch(theirsCommit)
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
		_, _ = fmt.Fprintln(opts.Progress, patch.Stats())
		return r.Storer.SetReference(plumbing.NewHashReference(head.Name(), ref.Hash()))

	case FastForwardMerge:
		if ff {
			_, _ = fmt.Fprintf(
				opts.Progress,
				"Updating %s...%s\nFast-forward\n%s",
				head.Hash().String()[:7],
				ref.Hash().String()[:7],
				patch.Stats(),
			)
			return r.Storer.SetReference(plumbing.NewHashReference(head.Name(), ref.Hash()))
		}
		fallthrough

	case OrtMerge:
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
			path := change.To.Name
			if path == "" {
				path = change.From.Name
			}
			pair := changes[path]
			pair.ours = change
			changes[path] = pair
		}

		for _, change := range baseToTheirs {
			path := change.To.Name
			if path == "" {
				path = change.From.Name
			}
			pair := changes[path]
			pair.theirs = change
			changes[path] = pair
		}

		w, err := r.Worktree()
		if err != nil {
			return err
		}

		mergeHasConflict := false

		for filepath, pair := range changes {
			var base, ours, theirs *object.File
			var baseReader, oursReader, theirsReader io.ReadCloser

			// Only our file has changed
			if pair.ours != nil && pair.theirs == nil {
				action, err := pair.ours.Action()
				if err != nil {
					return err
				}

				switch action {

				case merkletrie.Insert, merkletrie.Modify:
					_, ours, err = pair.ours.Files()
					if err != nil {
						return err
					}

					oursReader, err = ours.Reader()
					if err != nil {
						return err
					}

					var dst io.Writer
					dst, err = w.Filesystem.Create(filepath)
					if err != nil {
						return err
					}
					if _, err = io.Copy(dst, oursReader); err != nil {
						return err
					}
					if _, err = w.Add(filepath); err != nil {
						return err
					}

				// Our file was deleted
				case merkletrie.Delete:
					if err = w.Filesystem.Remove(filepath); err != nil && !os.IsNotExist(err) {
						return err
					}
					if _, err = w.Remove(filepath); err != nil && !errors.Is(err, index.ErrEntryNotFound) {
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
					_, theirs, err = pair.theirs.Files()
					if err != nil {
						return err
					}

					theirsReader, err = theirs.Reader()
					if err != nil {
						return err
					}

					var dst io.Writer
					dst, err := w.Filesystem.Create(filepath)
					if err != nil {
						return err
					}
					if _, err = io.Copy(dst, theirsReader); err != nil {
						return err
					}
					if _, err = w.Add(filepath); err != nil {
						return err
					}

				// Their file has been deleted
				case merkletrie.Delete:
					if err = w.Filesystem.Remove(filepath); err != nil && !os.IsNotExist(err) {
						return err
					}
					if _, err = w.Remove(filepath); err != nil && !errors.Is(err, index.ErrEntryNotFound) {
						return err
					}
				}
			}

			// Both changed the file
			if pair.ours != nil && pair.theirs != nil {
				base, ours, err = pair.ours.Files()
				if err != nil {
					return err
				}
				_, theirs, err = pair.theirs.Files()
				if err != nil {
					return err
				}

				var ourAction, theirAction merkletrie.Action
				ourAction, err = pair.ours.Action()
				if err != nil {
					return err
				}

				theirAction, err = pair.theirs.Action()
				if err != nil {
					return err
				}

				switch {

				// Added or Modified by both
				case ourAction == merkletrie.Modify && theirAction == merkletrie.Modify,
					ourAction == merkletrie.Insert && theirAction == merkletrie.Insert:

					// // If they made the same changes
					if ours.Hash == theirs.Hash {
						continue // Skip
					}

					baseReader, err = base.Reader()
					if err != nil {
						return err
					}
					defer func() { _ = baseReader.Close() }()

					var oursReader io.ReadCloser
					oursReader, err = ours.Reader()
					if err != nil {
						return err
					}
					defer func() { _ = oursReader.Close() }()

					_, theirs, err = pair.theirs.Files()
					if err != nil {
						return err
					}

					theirsReader, err = theirs.Reader()
					if err != nil {
						return err
					}
					defer func() { _ = theirsReader.Close() }()

					mergeResult, err := diff3.Merge(oursReader, baseReader, theirsReader, true, head.Name().Short(), ref.Name().Short())
					if err != nil {
						return err
					}

					file, err := w.Filesystem.Create(filepath)
					if err != nil {
						return err
					}
					defer func() { _ = file.Close() }()

					if _, err = io.Copy(file, mergeResult.Result); err != nil {
						return err
					}
					if !mergeResult.Conflicts {
						if _, err = w.Add(filepath); err != nil {
							return err
						}
					}

					mergeHasConflict = mergeHasConflict || mergeResult.Conflicts

					// Deleted by both
				case ourAction == merkletrie.Delete && theirAction == merkletrie.Delete:
					if err = w.Filesystem.Remove(filepath); err != nil && !os.IsNotExist(err) {
						return err
					}
					if _, err = w.Remove(filepath); err != nil && !errors.Is(err, index.ErrEntryNotFound) {
						return err
					}

					// Inserted / Modified by us, deleted by them
				case (ourAction == merkletrie.Insert || ourAction == merkletrie.Modify) && theirAction == merkletrie.Delete:
					var dst io.Writer
					dst, err = w.Filesystem.Create(filepath)
					if err != nil {
						return err
					}

					oursReader, err = ours.Reader()
					if err != nil {
						return err
					}
					if _, err = io.Copy(dst, oursReader); err != nil {
						return err
					}
					if _, err = w.Add(filepath); err != nil {
						return err
					}

					// Inserted / Modified by them, deleted by us
				case (theirAction == merkletrie.Insert || theirAction == merkletrie.Modify) && ourAction == merkletrie.Delete:
					dstFile, err := w.Filesystem.Create(filepath)
					if err != nil {
						return err
					}
					var theirsReader io.ReadCloser
					theirsReader, err = theirs.Reader()
					if err != nil {
						return err
					}
					if _, err = io.Copy(dstFile, theirsReader); err != nil {
						return err
					}
					if _, err = w.Add(filepath); err != nil {
						return err
					}
				}
			}

		}

		if mergeHasConflict {
			return ErrMergeConflict
		}

		status, err := w.Status()
		if err != nil {
			return err
		}

		if status.IsClean() {
			return nil
		}

		var newHash plumbing.Hash
		newHash, err = w.Commit(
			fmt.Sprintf("Merge ... with %s", ref.Name()),
			&git.CommitOptions{
				Author:    &oursCommit.Author,
				Committer: &oursCommit.Committer,
				Parents:   []plumbing.Hash{oursCommit.Hash, theirsCommit.Hash},
			},
		)

		var newCommit *object.Commit
		newCommit, err = r.CommitObject(newHash)
		if err != nil {
			return err
		}

		patch, err = oursCommit.Patch(newCommit)
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintf(opts.Progress, "Merge made by the 'ort' strategy.\n%s", patch.Stats())
	default:
		return git.ErrUnsupportedMergeStrategy
	}

	return err
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
