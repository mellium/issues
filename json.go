// Copyright 2018 The Mellium Contributors.
// Use of this source code is governed by the BSD 2-clause
// license that can be found in the LICENSE file.

package main

// export represents the contents of a Bitbucket export file.
// It was autogenerated by https://mholt.github.io/json-to-go/ and then tweaked
// a bit, but there are some fields that don't have the correct type because my
// exports didn't use them.
type export struct {
	Milestones  []interface{} `json:"milestones"`
	Attachments []interface{} `json:"attachments"`
	Versions    []interface{} `json:"versions"`
	Comments    []struct {
		Content   *string    `json:"content"`
		CreatedOn time.Time  `json:"created_on"`
		User      string     `json:"user"`
		UpdatedOn *time.Time `json:"updated_on"`
		Issue     int        `json:"issue"`
		ID        int        `json:"id"`
	} `json:"comments"`
	Meta struct {
		DefaultMilestone interface{} `json:"default_milestone"`
		DefaultAssignee  interface{} `json:"default_assignee"`
		DefaultKind      string      `json:"default_kind"`
		DefaultComponent interface{} `json:"default_component"`
		DefaultVersion   interface{} `json:"default_version"`
	} `json:"meta"`
	Components []struct {
		Name string `json:"name"`
	} `json:"components"`
	Issues []struct {
		Status           string        `json:"status"`
		Priority         string        `json:"priority"`
		Kind             string        `json:"kind"`
		ContentUpdatedOn time.Time     `json:"content_updated_on"`
		Voters           []interface{} `json:"voters"`
		Title            string        `json:"title"`
		Reporter         string        `json:"reporter"`
		Component        *string       `json:"component"`
		Watchers         []string      `json:"watchers"`
		Content          string        `json:"content"`
		Assignee         interface{}   `json:"assignee"`
		CreatedOn        time.Time     `json:"created_on"`
		Version          interface{}   `json:"version"`
		EditedOn         interface{}   `json:"edited_on"`
		Milestone        interface{}   `json:"milestone"`
		UpdatedOn        time.Time     `json:"updated_on"`
		ID               int           `json:"id"`
	} `json:"issues"`
	Logs []struct {
		Comment     int       `json:"comment"`
		ChangedTo   string    `json:"changed_to"`
		Field       string    `json:"field"`
		CreatedOn   time.Time `json:"created_on"`
		User        string    `json:"user"`
		Issue       int       `json:"issue"`
		ChangedFrom string    `json:"changed_from"`
	} `json:"logs"`
}