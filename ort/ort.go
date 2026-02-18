package ort

import (
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
)

type MergeStrategy git.MergeStrategy

const (
	FastForwardOnly MergeStrategy = iota
	FastForwardMerge
	OrtMerge
)

type MergeOptions struct {
	Strategy               MergeStrategy
	OrtMergeStrategyOption git.OrtMergeStrategyOption
}

func Merge(r *git.Repository, ref plumbing.Reference, opts MergeOptions) error {
	return nil
}
