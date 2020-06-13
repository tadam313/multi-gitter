package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/lindell/multi-gitter/internal/domain"
)

// Github contain github configuration
type Github struct {
	BaseURL string
	Token   string // Personal access token
}

// DefaultConfig contains values for the github.com api
// The access token is still always needed
var DefaultConfig = Github{
	BaseURL: "https://api.github.com/",
}

type createPrRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

type pr struct {
	ID     int `json:"id"`
	Number int `json:"number"`
}

type addReviewersRequest struct {
	Reviewers []string `json:"reviewers"`
}

type repository struct {
	SSH           string `json:"ssh_url"`
	Slug          string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`

	Archived bool `json:"archived"`
	Disabled bool `json:"disabled"`
}

func (r repository) GetURL() string {
	return r.SSH
}

func (r repository) GetBranch() string {
	return r.DefaultBranch
}

// GetRepositories fetches repositories from and organization
func (g Github) GetRepositories(orgName string) ([]domain.Repository, error) {
	allRepos := []domain.Repository{}
	for i := 1; ; i++ {
		repos, err := g.getRepositories(orgName, i)
		if err != nil {
			return nil, err
		} else if len(repos) == 0 {
			break
		}
		allRepos = append(allRepos, repos...)
	}
	return allRepos, nil
}

func (g Github) getRepositories(orgName string, page int) ([]domain.Repository, error) {
	q := url.Values{
		"page":     []string{fmt.Sprint(page)},
		"per_page": []string{"100"},
	}

	url := fmt.Sprintf("%sorgs/%s/repos?"+q.Encode(), g.BaseURL, orgName)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "token "+g.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, responseToError(resp, "cloud not fetching repositories")
	}

	var rr []repository
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return nil, err
	}

	// Transform the slice of repositories struct into a slice of the interface repositories
	repos := make([]domain.Repository, 0, len(rr))
	for _, r := range rr {
		if !r.Archived && !r.Disabled {
			repos = append(repos, r)
		}
	}
	return repos, nil
}

// CreatePullRequest creates a pull request
func (g Github) CreatePullRequest(repo domain.Repository, newPR domain.NewPullRequest) error {
	repository, ok := repo.(repository)
	if !ok {
		return errors.New("the repository needs to originate from this package")
	}

	pr, err := g.createPullRequest(repository, newPR)
	if err != nil {
		return err
	}

	if err := g.addReviewers(repository, newPR, pr); err != nil {
		return err
	}

	return nil
}

func (g Github) createPullRequest(repo repository, newPR domain.NewPullRequest) (pr, error) {
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(createPrRequest{
		Title: newPR.Title,
		Body:  newPR.Body,
		Head:  newPR.Head,
		Base:  newPR.Base,
	})

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%srepos/%s/pulls", g.BaseURL, repo.Slug), buf)
	if err != nil {
		return pr{}, err
	}
	req.Header.Add("Authorization", "token "+g.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return pr{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return pr{}, responseToError(resp, "could not create pull request")
	}

	var pullRequest pr
	if err := json.NewDecoder(resp.Body).Decode(&pullRequest); err != nil {
		return pr{}, err
	}
	return pullRequest, nil
}

func (g Github) addReviewers(repo repository, newPR domain.NewPullRequest, createdPR pr) error {
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(addReviewersRequest{
		Reviewers: newPR.Reviewers,
	})

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%srepos/%s/pulls/%d/requested_reviewers", g.BaseURL, repo.Slug, createdPR.Number), buf)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", "token "+g.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return responseToError(resp, "could not add reviewers to pull request")
	}

	return nil
}
