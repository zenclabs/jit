package main

import (
	"flag"
	"fmt"
	"github.com/andygrunwald/go-jira"
	"github.com/deckarep/golang-set"
	"github.com/fatih/color"
	"github.com/libgit2/git2go"
	"gopkg.in/AlecAivazis/survey.v1"
	"log"
	"os"
	"regexp"
	"strings"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	repoPath := flag.String("repo", cwd, "Path to the git repository")
	jiraHost := flag.String("jira-host", "", "URL of the Jira Cloud host")
	jiraUsername := flag.String("jira-user", "", "Jira username")
	jiraToken := flag.String("jira-token", "", "Jira API token")
	flag.Parse()

	if *jiraHost == "" {
		log.Fatal("Please set a Jira host with --jira-host.")
	}
	if *jiraUsername == "" {
		log.Fatal("Please set a Jira username with --jira-user.")
	}
	if *jiraToken == "" {
		log.Fatal("Please set a Jira API token with --jira-token.")
	}

	repo, err := git.OpenRepository(*repoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Free()
	jiraKeysFromBranches := mapset.NewSet()
	jiraKeyToBranchName := make(map[string]string)
	branchIterator, err := repo.NewBranchIterator(git.BranchLocal)
	if err != nil {
		log.Fatal(err)
	}
	defer branchIterator.Free()
	jiraIssueFormat, err := regexp.Compile("^([A-Z]+-[0-9]+)")
	if err != nil {
		log.Fatal(err)
	}
	err = branchIterator.ForEach(func(b *git.Branch, t git.BranchType) error {
		name, err := b.Name()
		if err != nil {
			return err
		}
		res := jiraIssueFormat.FindStringSubmatch(name)
		if len(res) > 0 {
			jiraKey := res[1]
			jiraKeysFromBranches.Add(jiraKey)
			jiraKeyToBranchName[jiraKey] = name
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	tp := jira.BasicAuthTransport{
		Username: strings.TrimSpace(*jiraUsername),
		Password: strings.TrimSpace(*jiraToken),
	}
	api, err := jira.NewClient(tp.Client(), *jiraHost)
	if err != nil {
		log.Fatal(err)
	}
	issues, _, err := api.Issue.Search("assignee = currentUser()", nil)
	if err != nil {
		log.Fatal(err)
	}
	choices := make([]string, len(issues))
	for i, issue := range issues {
		var colouredPriority string
		switch issue.Fields.Priority.Name {
		case "Low":
			colouredPriority = color.GreenString(issue.Fields.Priority.Name)
		case "Medium":
			colouredPriority = color.BlueString(issue.Fields.Priority.Name)
		case "High":
			colouredPriority = color.MagentaString(issue.Fields.Priority.Name)
		case "Critical":
		case "Blocker":
			colouredPriority = color.RedString(issue.Fields.Priority.Name)
		default:
			colouredPriority = color.GreenString(issue.Fields.Priority.Name)
		}
		var summary string
		if jiraKeysFromBranches.Contains(issue.Key) {
			summary = fmt.Sprintf("*%s %v (%s): %+v", issue.Key, colouredPriority, issue.Fields.Type.Name, issue.Fields.Summary)
		} else {
			summary = fmt.Sprintf("%s %v (%s): %+v", issue.Key, colouredPriority, issue.Fields.Type.Name, issue.Fields.Summary)
		}
		choices[i] = summary
	}
	var pickedIssue string
	err = survey.AskOne(&survey.Select{
		Message: "Choose an issue:",
		Options: choices,
	}, &pickedIssue, nil)
	if err != nil {
		log.Fatal(err)
	}
	jiraKey := strings.TrimPrefix(strings.Split(pickedIssue, " ")[0], "*")
	var branchName *string
	if jiraKeysFromBranches.Contains(jiraKey) {
		// Switch to the existing branch.
		b := jiraKeyToBranchName[jiraKey]
		branchName = &b
	} else {
		// Create a new branch.
		branchName, err = createBranch(repo, jiraKey)
		if err != nil {
			log.Fatal(err)
		}
	}
	err = checkoutBranch(repo, *branchName)
	if err != nil {
		log.Fatal(err)
	}
}

func createBranch(repo *git.Repository, jiraKey string) (*string, error) {
	var branchName string
	survey.AskOne(&survey.Input{
		Message: "Enter a branch name:",
		Default: jiraKey,
	}, &branchName, nil)
	master, err := repo.LookupBranch("origin/master", git.BranchRemote)
	if err != nil {
		return nil, err
	}
	defer master.Free()
	lastCommit, err := repo.LookupCommit(master.Target())
	if err != nil {
		return nil, err
	}
	defer lastCommit.Free()
	branch, err := repo.CreateBranch(branchName, lastCommit, false)
	if err != nil {
		return nil, err
	}
	defer branch.Free()
	return &branchName, err
}

func checkoutBranch(repo *git.Repository, branchName string) error {
	branch, err := repo.LookupBranch(branchName, git.BranchLocal)
	if err != nil {
		return err
	}
	defer branch.Free()

	commit, err := repo.LookupCommit(branch.Target())
	if err != nil {
		return err
	}
	defer commit.Free()

	tree, err := repo.LookupTree(commit.TreeId())
	if err != nil {
		return err
	}
	defer tree.Free()

	err = repo.CheckoutTree(tree, &git.CheckoutOpts{
		Strategy: git.CheckoutSafe,
	})
	if err != nil {
		return err
	}
	repo.SetHead("refs/heads/" + branchName)
	return nil
}
