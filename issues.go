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
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
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

type dummyCloser struct{}

func (dummyCloser) Close() error {
	return nil
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

	// Parse the issues URL
	importURL, err := url.Parse(fmt.Sprintf("https://api.github.com/repos/%s/%s/import/issues", owner, repo))
	if err != nil {
		logger.Panicf("Error parsing import URL: `%v'\n", err)
	}

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

importloop:
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

		closed := false
		switch strings.ToLower(issue.Status) {
		case "resolved", "closed", "invalid", "wontfix", "duplicate":
			closed = true
		case "new", "open", "on hold", "onhold":
		default:
			debug.Printf("Found unknown status on issue #%d: `%s'", issue.ID, issue.Status)
		}

		debug.Printf("Attempting to create issue %d\n", issue.ID)
		req := struct {
			Issue    ghIssue     `json:"issue"`
			Comments []ghComment `json:"comments"`
		}{
			Issue: ghIssue{
				Title: issue.Title,
				Body: fmt.Sprintf(`by **%s**:

---

%s`, issue.Reporter, issue.Content),
				Labels: labels,
				Closed: closed,
			},
			Comments: []ghComment{},
		}
		reqBytes, err := json.Marshal(req)
		if err != nil {
			logger.Printf("Error marshaling GitHub issue %d: `%v'\n", issue.ID, err)
			errors++
			wait(resp, debug)
			continue
		}

		// This may break at any time.
		// See: https://gist.github.com/jonmagic/5282384165e0f86ef105
		result := ghResponse{}
		resp, err = client.Do(context.TODO(), &http.Request{
			Method: "POST",
			URL:    importURL,
			Header: map[string][]string{
				"Accept": []string{"application/vnd.github.golden-comet-preview+json"},
			},
			Body: struct {
				io.Reader
				io.Closer
			}{
				Reader: bytes.NewReader(reqBytes),
				Closer: dummyCloser{},
			},
			ContentLength: int64(len(reqBytes)),
		}, &result)
		switch err.(type) {
		case *github.AcceptedError:
			d := json.NewDecoder(resp.Body)
			_ = d.Decode(result)
		case nil:
		default:
			errors++
			logger.Printf("Error creating issue %d: `%v'\n", issue.ID, err)
			debug.Printf("Code: `%s'\n", resp.Status)
			wait(resp, debug)
			continue
		}

		debug.Printf("Status of %d: `%+v'\n", issue.ID, result)
		panic("DONE")
		// Poll for the issue until the import is finished.
		issueURL, err := url.Parse(result.URL)
		if err != nil {
			logger.Printf("Failed to parse issue URL from GitHub, skipping import verification for issue %d: `%s'\n", issue.ID, issueURL)
			wait(resp, debug)
			errors++
			continue
		}
		for result.Status == "pending" {
			debug.Printf("Attempting to verify issue creation for %d…\n", issue.ID)
			// TODO: verify that GitHub doesn't make us POST to another domain.
			result = ghResponse{}
			resp, err = client.Do(context.TODO(), &http.Request{
				Method: "GET",
				URL:    issueURL,
				Header: map[string][]string{
					"Accept": []string{"application/vnd.github.golden-comet-preview+json"},
				},
			}, &result)
			if err != nil {
				errors++
				logger.Printf("Error creating issue %d: `%v'\n", issue.ID, err)
				debug.Printf("Result: `%+v'\n", result)
				wait(resp, debug)
				continue importloop
			}
		}
		imported++
		wait(resp, debug)
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
