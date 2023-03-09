package storage

import (
	"fmt"
	"os"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func Download() error {
	return nil
}

func (s *Storage) upToDateIfLocalClean() error {

	repo, err := git.PlainOpen(s.path)
	if err != nil {
		return err
	}

	// Get the working tree
	wt, err := repo.Worktree()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return err
	}

	// Check if the working tree is clean
	status, err := wt.Status()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return err
	}

	if status.IsClean() {
		// Pull the latest changes
		err = wt.Pull(&git.PullOptions{
			RemoteName: s.gitCloneOptions.RemoteName,
			Progress:   os.Stdout,
			Auth:       s.gitCloneOptions.Auth,
		})

		if err != nil {
			if err == git.NoErrAlreadyUpToDate {
				fmt.Println("Already up to date.")
				return nil
			} else {
				fmt.Fprintf(os.Stderr, "worktree pull error: %s\n", err)
				return err
			}
		}
	} else {
		fmt.Println("Local file(s) has changes, cancel sync from remote")
	}

	return nil
}

func (s *Storage) commitAndPushLocalChanges() error {

	repo, err := git.PlainOpen(s.path)
	if err != nil {
		return err
	}

	// Get the working tree
	wt, err := repo.Worktree()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return err
	}

	// Add the changes to the index
	_, err = wt.Add(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return err
	}

	// Commit the changes
	commit, err := wt.Commit("Refresh store secrets", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "apkgo",
			Email: "support@apkgo.com.cn",
			When:  time.Now(),
		},
		All: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return err
	}

	// Print the commit hash
	fmt.Printf("Committed changes: %s\n", commit.String())

	// Push the changes
	err = repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/master:refs/heads/master"),
		},
		Auth: s.gitCloneOptions.Auth,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return err
	}

	fmt.Println("Changes pushed successfully")
	return nil

}
