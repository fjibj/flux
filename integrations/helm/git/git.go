package git

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"time"

	gogit "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

const (
	DefaultCloneTimeout = 2 * time.Minute
)

var (
	ErrNoChanges    = errors.New("no changes made in repo")
	ErrNoRepo       = errors.New("no repo provided")
	ErrNoRepoCloned = errors.New("no repo cloned")
)

type GitRemoteConfig struct {
	URL        string `json:"url"`
	Branch     string `json:"branch"`
	ConfigPath string `json:"config-path"`
	ChartsPath string `json:"charts-path"`
}

// Repo represents a (remote) git repo.
type Repo struct {
	GitRemoteConfig
}

// Checkout is a local clone of the remote repo.
type Checkout struct {
	logger   log.Logger
	Config   GitRemoteConfig
	Dir      string
	repo     *gogit.Repository
	worktree *gogit.Worktree
	sync.RWMutex
}

func NewGitRemoteConfig(url, branch, configPath, chartsPath string) (GitRemoteConfig, error) {
	if len(configPath) == 0 || configPath[0] == '/' {
		return GitRemoteConfig{}, errors.New("git subdirectory (--git-config-path) must be given and cannot have leading forward slash")
	}
	if len(chartsPath) == 0 || chartsPath[0] == '/' {
		return GitRemoteConfig{}, errors.New("git subdirectory (--git-charts-path) must be given and cannot have leading forward slash")
	}
	return GitRemoteConfig{
		URL:        url,
		Branch:     branch,
		ConfigPath: configPath,
		ChartsPath: chartsPath,
	}, nil
}

// CloneAndCheckout creates a local clone of a remote repo and
// checks out the relevant branch
//		subdir reflects whether we are:
//																		* acting on Custom Resource change
//																		* acting on Charts changes (syncing the cluster when there were only commits
//																		  in the Charts parts of the repo which did not trigger Custom Resource changes)
func (ch *Checkout) CloneAndCheckout(ctx context.Context, cloneSubdir string) error {
	ch.Lock()
	defer ch.Unlock()

	if ch.Config.URL == "" {
		return ErrNoRepo
	}

	repoDir, err := ioutil.TempDir(os.TempDir(), cloneSubdir)
	if err != nil {
		return err
	}

	ch.Dir = repoDir

	repo, err := gogit.PlainClone(repoDir, false, &gogit.CloneOptions{
		URL: ch.Config.URL,
	})
	if err != nil && err != gogit.ErrRepositoryAlreadyExists {
		return err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return err
	}

	br := ch.Config.Branch
	if br == "" {
		br = "master"
	}
	err = wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", br)),
	})
	if err != nil {
		return err
	}

	ch.repo = repo
	ch.worktree = wt

	return nil
}

// Cleanup removes the temp repo directory
func (ch *Checkout) Cleanup() {
	ch.Lock()
	defer ch.Unlock()

	if ch.Dir != "" {
		os.Remove(ch.Dir)
	}
	ch.Dir = ""
	ch.repo = nil
	ch.worktree = nil
}

/*
// ChangedCharts makes a new git pull and determines which charts changed
// ChangedCharts method does a git pull and finds the latest revisison
// Among suppplied custom resources it finds the ones whose revision is different
// Charts related to these custom resources need to be released
func (ch *Checkout) ChangedCharts(crs []ifv1.FluxHelmResource) ([]ifv1.FluxHelmResource, error) {
	ch.Lock()
	defer ch.Unlock()

	rev, err := ch.getRevision()
	if err != nil {
		return nil, err
	}

	if err := ch.pull(); err != nil {
		return nil, err
	}

		rev, err := ch.getRevision()
		if err != nil {
			return nil, err
		}

		var crForUpdate []ifv1.FluxHelmResource
		for _, cr := range crs {
				crstatus := cr.Status.Revision
				// When a new Custom Resource is created, Status.Revision
				if crstatus != "" && crstatus != rev {
					crForUpdate = append(crForUpdate, cr)
				}
		}

	return []ifv1.FluxHelmResource{}, nil
}
*/

func (ch *Checkout) pull() error {
	w := ch.worktree
	if w == nil {
		return ErrNoRepoCloned
	}
	err := w.Pull(&gogit.PullOptions{RemoteName: "origin"})
	return err
}

/*
// getRevision returns string representation of the revision hash
func (ch *Checkout) getRevision() (string, error) {
	if ch.repo == nil {
		return "", ErrNoRepoCloned
	}
	ref, err := ch.repo.Head()
	if err != nil {
		return "", err
	}
	rev := ref.Hash().String()
	return rev, nil
}

*/
