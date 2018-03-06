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
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

// defTmpl is the default issue template; used if none is provided by the user.
const defTmpl = `
	|  Metadata  | Value  |
	| ---------- | ------ |
	{{if .Reporter}}| Reporter   | **{{ .Reporter }}** |
	{{end -}}
	{{if .CreatedOn}}| Created On | **{{ .CreatedOn }}** |
	{{end -}}
	{{if .EditedOn}} | Edited On  | **{{ .EditedOn }}** |
	{{end -}}
	{{if .UpdatedOn}}| Updated On | **{{ .UpdatedOn }}** |
	{{end -}}
	
	---
	
	{{if .Content}}{{.Content}}{{end}}`

func usage(flags *flag.FlagSet, token string) {
	t := time.Now()
	proj := "MyProject"
	jsonIssue, _ := json.MarshalIndent(issue{
		Status:           "open",
		Priority:         "high",
		Kind:             "bug",
		ContentUpdatedOn: &t,
		Title:            "An issue title",
		Reporter:         "YourUsername",
		Component:        &proj,
		Content:          "This is the body of the issue",
		CreatedOn:        t,
		EditedOn:         &t,
		UpdatedOn:        &t,
		ID:               123,
	}, "\t", "  ")
	fmt.Fprintf(flags.Output(), `Usage of %s:

	issues [options] zip repo

The zip file is a Bitbucket issue export which can be obtained by visiting your
repo's settings on Bitbucket and choosing "Import & export" from the "issues"
section.

Templates:

When creating issues on GitHub, a template is used. If the user does not specify
a template, the default template is used:

%s

The data provided to the template (in JSON format) is:

	%s

For more information see the documentation for the Go package text/template:
https://godoc.org/text/template

Environment:	

	GITHUB_TOKEN=%s

Options:

`, os.Args[0], defTmpl, jsonIssue, token)
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
		tmpl   = ""
		labels = ""
	)
	flags := flag.NewFlagSet("issues", flag.ContinueOnError)
	flags.BoolVar(&help, "help", help, "print this help message")
	flags.BoolVar(&h, "h", h, "print this help message")
	flags.BoolVar(&v, "v", v, "enable verbose debug logging")
	flags.StringVar(&tmpl, "f", tmpl, "a template to use for the contents of new issues")
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

	if tmpl == "" {
		tmpl = defTmpl
	}
	issueTmpl := template.Must(template.New("issue").Parse(tmpl))

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

	// Create the issues.
	var (
		imported = 0
		errors   = 0
	)
	sort.Sort(issues.Issues)

	ghissues, resp, err := client.Issues.ListByRepo(context.TODO(), owner, repo, &github.IssueListByRepoOptions{
		State: "all",
	})
	if err != nil {
		logger.Fatalf("Error enumerating existing issues on repo: `%s'\n", err)
	}
	n := len(ghissues)
	debug.Printf("Skipping %d existing issues\n", n)
	if len(issues.Issues) <= n {
		issues.Issues = issues.Issues[len(issues.Issues):]
	} else {
		issues.Issues = issues.Issues[n:]
	}
	wait(resp, debug)

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
		buf := new(bytes.Buffer)
		err = issueTmpl.Execute(buf, issue)
		if err != nil {
			logger.Printf("Error executing issue template on issue %d: `%v'", issue.ID, err)
		}
		req := github.IssueRequest{
			Title: github.String(issue.Title),
			Body:  github.String(buf.String()),
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
		wait(resp, debug)

		if err == nil && state == "closed" && is.Number != nil {
			debug.Printf("Closing issue %d…\n", issue.ID)
			_, resp, err = client.Issues.Edit(context.TODO(), owner, repo, *is.Number, &github.IssueRequest{
				State: github.String(state),
			})
			if err != nil {
				errors++
				logger.Printf("Error closing issue %d: `%v'\n", issue.ID, err)
			} else {
				imported++
			}
			wait(resp, debug)
		}
	}

	s, _, _ := client.Octocat(context.TODO(), fmt.Sprintf("Imported %d, Errors %d", imported, errors))
	fmt.Fprintln(os.Stderr, s)
}

// wait attempts to parse the Retry-After header and then sleeps the correct
// amount of time.
// If no Retry-After header exists, it defaults to sleeping 1 second.
func wait(resp *github.Response, debug *log.Logger) {
	// GitHub asks that you wait at least one second between requests:
	// https://developer.github.com/v3/guides/best-practices-for-integrators/#dealing-with-abuse-rate-limits
	retry := resp.Header.Get("Retry-After")
	var waittime time.Duration
	if retry == "" {
		// Default to 1 second if the Retry-After header was not set.
		waittime = time.Second
	} else {
		// GitHub's documentation shows numbers with no unit, but it appears to
		// actually be using a unit. Support both, just in case.
		n, err := strconv.Atoi(retry)
		if err == nil {
			waittime = time.Duration(n) * time.Second
		} else {
			waittime, err = time.ParseDuration(retry)
			if err != nil {
				debug.Printf("Error parsing Retry-After value `%s': `%v'\n", waittime, err)
				waittime = time.Second
			}
		}
	}
	debug.Printf("Waiting %s between requests…\n", waittime)
	time.Sleep(waittime)
}
