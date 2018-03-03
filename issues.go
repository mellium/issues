// Copyright 2018 The Mellium Contributors.
// Use of this source code is governed by the BSD 2-clause
// license that can be found in the LICENSE file.

// The issues command migrates issues from Bitbucket to GitHub.
//
// For more information try:
//
//     issues -help
package main // import "mellium.im/issues"

import (
	"archive/zip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

func usage(flags *flag.FlagSet, token string) {
	fmt.Fprintf(flags.Output(), `Usage of %s:

	issues [options] zip repo

The zip file is a Bitbucket issue export which can be obtained by visiting your
repo's settings on Bitbucket and choosing "Import & export" from the "issues"
section.

Environment:	

	GITHUB_TOKEN=%s

Options:

`, os.Args[0], token)
	flags.PrintDefaults()
}

func main() {
	// Setup loggers
	logger := log.New(os.Stderr, "mellium.im/issues ", log.LstdFlags)
	debug := log.New(ioutil.Discard, "mellium.im/issues DEBUG ", log.LstdFlags)

	// Setup and parse command line flags
	var (
		help   = false
		h      = false
		v      = false
		token  = os.Getenv("GITHUB_TOKEN")
		labels = ""
	)
	flags := flag.NewFlagSet("issues", flag.ContinueOnError)
	flags.BoolVar(&help, "help", help, "print this help message")
	flags.BoolVar(&h, "h", h, "print this help message")
	flags.BoolVar(&v, "v", v, "enable verbose debug logging")
	flags.StringVar(&labels, "labels", labels, "list of comma separated labels to apply to all imported issues")
	if err := flags.Parse(os.Args[1:]); err != nil {
		logger.Fatalf("Error while parsing flags: `%v'", err)
	}

	switch {
	case h || help:
		flags.SetOutput(os.Stdout)
		usage(flags, token)
		return
	case token == "":
		logger.Println("GITHUB_TOKEN cannot be empty")
		flags.SetOutput(os.Stderr)
		usage(flags, token)
		return
	case flags.NArg() < 2:
		flags.SetOutput(os.Stderr)
		usage(flags, token)
		return
	}
	if v {
		debug.SetOutput(os.Stderr)
	}

	// Split the repo name into owner and repo
	args := flags.Args()
	idx := strings.IndexByte(args[1], '/')
	if idx < 1 || idx == len(args[1])-1 {
		logger.Fatalf("Invalid repo name, expected: owner/repo")
	}
	owner := args[1][:idx]
	repo := args[1][idx+1:]

	// Open the zip file
	r, err := zip.OpenReader(args[0])
	if err != nil {
		logger.Fatalf("Error opening `%s': `%v'", args[0], err)
	}
	defer func() {
		err := r.Close()
		if err != nil {
			debug.Printf("Error closing `%s': `%v'", args[0], err)
		}
	}()

	// Just curious, doesn't look like they currently set anything though.
	if r.Comment != "" {
		debug.Printf("Comment found in zip file: `%s'\n", r.Comment)
	}

	// Find the JSON file in the zip file.
	var f *zip.File
	for _, f = range r.File {
		if f.Name != "db-1.0.json" {
			debug.Printf("Skipping file `%s/%s'…", args[0], f.Name)
			continue
		}

		debug.Printf("Found `%s/%s'…", args[0], f.Name)
		break
	}

	// Open the JSON file in the zip file.
	rc, err := f.Open()
	if err != nil {
		logger.Fatalf("Error opening JSON from archive: `%v'", err)
	}

	// Parse the JSON file.
	issues := export{}
	err = json.NewDecoder(rc).Decode(&issues)
	if err != nil {
		logger.Fatalf("Error decoding JSON from archive: `%v'", err)
	}
	rc.Close()

	// Log in to GitHub
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(context.TODO(), ts)
	client := github.NewClient(tc)

	// TODO: If issues already exist, prompt before attempting to create new ones.

	// 		Title            string        `json:"title"`
	// 		Content          string        `json:"content"`
	// 		Priority         string        `json:"priority"`
	// 		Kind             string        `json:"kind"`

	// 		Status           string        `json:"status"`
	// 		ContentUpdatedOn time.Time     `json:"content_updated_on"`
	// 		Voters           []interface{} `json:"voters"`
	// 		Reporter         string        `json:"reporter"`
	// 		Component        *string       `json:"component"`
	// 		Watchers         []string      `json:"watchers"`
	// 		Assignee         interface{}   `json:"assignee"`
	// 		CreatedOn        time.Time     `json:"created_on"`
	// 		Version          interface{}   `json:"version"`
	// 		EditedOn         interface{}   `json:"edited_on"`
	// 		Milestone        interface{}   `json:"milestone"`
	// 		UpdatedOn        time.Time     `json:"updated_on"`
	// 		ID               int           `json:"id"`

	// Create the issues.
	var (
		imported = 0
		errors   = 0
	)
	sort.Sort(issues.Issues)
	for _, issue := range issues.Issues {
		labels := strings.Split(labels, ",")
		if issue.Priority != "" {
			labels = append(labels, issue.Priority)
		}
		if issue.Kind != "" {
			labels = append(labels, issue.Kind)
		}
		if issue.Component != nil && *issue.Component != "" {
			labels = append(labels, *issue.Component)
		}
		// Bitbucket status' are more fine grained than GitHub states, so set them
		// as a label in case we still need that information.
		if issue.Status != "" {
			labels = append(labels, issue.Status)
		}

		state := "open"
		switch strings.ToLower(issue.Status) {
		case "resolved", "closed", "invalid", "wontfix", "duplicate":
			state = "closed"
		case "new", "open", "on hold", "onhold":
		default:
			debug.Printf("Found unknown status on issue #%d: `%s'", issue.ID, issue.Status)
		}

		debug.Printf("Attempting to create issue %d\n", issue.ID)
		req := github.IssueRequest{
			Title: github.String(issue.Title),
			Body:  github.String(issue.Content),
			// TODO: labels are currently broken, I think I need to make sure they are
			// created first.
			// Labels: &labels,
			State: github.String(state),
		}
		is, resp, err := client.Issues.Create(context.TODO(), owner, repo, &req)
		if err != nil {
			errors++
			logger.Printf("Error creating issue %d: `%v'", issue.ID, err)
		} else {
			imported++
		}

		if err == nil && state == "closed" && is.Number != nil {
			logger.Printf("Closing issue %d…\n", issue.ID)
			_, _, err := client.Issues.Edit(context.TODO(), owner, repo, *is.Number, &github.IssueRequest{
				State: github.String(state),
			})
			if err != nil {
				errors++
				logger.Printf("Error closing issue %d: `%v'\n", issue.ID, err)
			} else {
				imported++
			}
		}

		// GitHub asks that you wait at least one second between requests:
		// https://developer.github.com/v3/guides/best-practices-for-integrators/#dealing-with-abuse-rate-limits
		retry := resp.Header.Get("Retry-After")
		waittime, err := time.ParseDuration(retry)
		switch {
		case err != nil:
			debug.Printf("Error parsing Retry-After value `%s': `%v'\n", waittime, err)
			waittime = time.Second
		case err == nil:
			debug.Printf("Waiting %s between requests…\n", waittime)
		}
		time.Sleep(waittime)
	}

	s, _, _ := client.Octocat(context.TODO(), fmt.Sprintf("Imported %d, Errors %d", imported, errors))
	fmt.Fprintln(os.Stderr, s)
}
