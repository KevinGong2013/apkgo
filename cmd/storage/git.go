package storage

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

func Download() error {
	return nil
}

func ensureRepoCleanAndUpToDate(repo *git.Repository) error {
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

	if !status.IsClean() {
		fmt.Fprintf(os.Stderr, "error: repository has uncommitted changes\n")
		return err
	}

	// Pull the latest changes
	err = wt.Pull(&git.PullOptions{
		RemoteName: "origin",
		Progress:   os.Stdout,
		Auth:       &http.BasicAuth{Username: "kevin", Password: "aoxianglele"},
	})

	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			fmt.Println("Repository is already up-to-date.")
		} else {
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
			return err
		}
	}

	// Get the latest commit hash
	return nil
}

func commitAndPushLocalChanges(repo *git.Repository) error {

	// Get the working tree
	wt, err := repo.Worktree()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	// Add the changes to the index
	_, err = wt.Add(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	// Commit the changes
	commit, err := wt.Commit("Update apkgo secrets", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Your Name",
			Email: "your-email@example.com",
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	// Print the commit hash
	fmt.Printf("Committed changes: %s\n", commit.String())

	// Push the changes
	err = repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/master:refs/heads/master"),
		},
		Auth: nil,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	fmt.Println("Changes pushed successfully")
	return nil

}
