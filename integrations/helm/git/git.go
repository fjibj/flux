package git

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/weaveworks/flux"
	ifv1 "github.com/weaveworks/flux/apis/integrations.flux/v1"
	gogit "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	yaml "gopkg.in/yaml.v2"
)

const (
	DefaultCloneTimeout = 2 * time.Minute
)

var (
	ErrNoChanges    = errors.New("no changes made in repo")
	ErrNoRepo       = errors.New("no repo provided")
	ErrNoRepoCloned = errors.New("no repo cloned")
)

// Repo represents a (remote) git repo.
type Repo struct {
	flux.GitRemoteConfig
}

// Checkout is a local clone of the remote repo.
type Checkout struct {
	logger   log.Logger
	Config   flux.GitRemoteConfig
	repo     *gogit.Repository
	worktree *gogit.Worktree
	Dir      string
	sync.RWMutex
}

// CloneAndCheckout creates a local clone of a remote repo and
// checks out the relevant branch
func (ch *Checkout) CloneAndCheckout(ctx context.Context) error {
	ch.Lock()
	defer ch.Unlock()

	if ch.Config.URL == "" {
		return ErrNoRepo
	}

	repoDir, err := ioutil.TempDir(os.TempDir(), "helmchart-gitclone")
	if err != nil {
		return err
	}

	ch.Dir = repoDir

	repo, err := gogit.PlainClone(repoDir, false, &gogit.CloneOptions{
		URL: ch.Config.URL,
		//		Progress: os.Stdout,
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
	if ch.Dir != "" {
		os.Remove(ch.Dir)
	}
	ch.Dir = ""
	ch.repo = nil
	ch.worktree = nil
}

// ChangedCharts makes a new git pull and determines which charts changed
// ChangedCharts method does a git pull and finds the latest revisison
// Among suppplied custom resources it finds the ones whose revision is different
// Charts related to these custom resources need to be released
func (ch *Checkout) ChangedCharts(crs []ifv1.FluxHelmResource) ([]ifv1.FluxHelmResource, error) {
	if err := ch.pull(); err != nil {
		return nil, err
	}
	rev, err := ch.getRevision()
	if err != nil {
		return nil, err
	}

	var crForUpdate []ifv1.FluxHelmResource
	for _, cr := range crs {
		/*
			crstatus := cr.Status.Revision
			// When a new Custom Resource is created, Status.Revision
			if crstatus != "" && crstatus != rev {
				crForUpdate = append(crForUpdate, cr)
			}
		*/
	}
	return []ifv1.FluxHelmResource{}, nil
}

func (ch *Checkout) pull() error {
	w := ch.worktree
	if w == nil {
		return ErrNoRepoCloned
	}
	err := w.Pull(&gogit.PullOptions{RemoteName: "origin"})
	return err
}

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

// getVersion returns string representation of the Chart version
// given a particular Chart path within the repo
func (ch *Checkout) getChartVersion(chartDir string) (string, error) {
	if ch.Dir == "" {
		return "", ErrNoRepoCloned
	}

	chartYaml, err := ioutil.ReadFile(filepath.Join(ch.Dir, chartDir, "Chart.yaml"))
	if err != nil {
		return "", err
	}

	type chartMeta struct {
		Version string `yaml:"version"`
	}
	chm := chartMeta{}
	err = yaml.Unmarshal(chartYaml, chm)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}

	return chm.Version, nil
}
