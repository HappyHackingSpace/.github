package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

type RepoStats struct {
	Name            string
	URL             string
	Stars           int
	Forks           int
	OpenIssues      int
	GoodFirstIssues int
	SecurityIssues  int
	RecentCommits   int
	LastCommit      string
}

type GhProjects struct {
	RepoStats
	XP     int
	Level  int
	Badges []string
}

func fetchOrgRepos(org, token string) ([]string, error) {
	var repos []string
	page := 1
	for {
		apiURL := fmt.Sprintf("https://api.github.com/orgs/%s/repos?per_page=100&page=%d", org, page)
		client := &http.Client{}
		req, _ := http.NewRequest("GET", apiURL, nil)
		if token != "" {
			req.Header.Set("Authorization", "token "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var data []struct {
			Name    string `json:"name"`
			HTMLURL string `json:"html_url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}
		if len(data) == 0 {
			break
		}
		for _, repo := range data {
			repos = append(repos, repo.HTMLURL)
		}
		page++
	}
	return repos, nil
}

func fetchRepoStats(repoURL, token string) (RepoStats, error) {
	ownerRepo := strings.TrimPrefix(repoURL, "https://github.com/")
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s", ownerRepo)
	client := &http.Client{}
	req, _ := http.NewRequest("GET", apiURL, nil)
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return RepoStats{}, err
	}
	defer resp.Body.Close()
	var data struct {
		StargazersCount int    `json:"stargazers_count"`
		ForksCount      int    `json:"forks_count"`
		OpenIssuesCount int    `json:"open_issues_count"`
		PushedAt        string `json:"pushed_at"`
		Name            string `json:"name"`
		HTMLURL         string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return RepoStats{}, err
	}

	// Good first issues
	issuesURL := fmt.Sprintf("https://api.github.com/repos/%s/issues?labels=good%20first%20issue&state=open", ownerRepo)
	req2, _ := http.NewRequest("GET", issuesURL, nil)
	if token != "" {
		req2.Header.Set("Authorization", "token "+token)
	}
	resp2, err := client.Do(req2)
	if err != nil {
		return RepoStats{}, err
	}
	defer resp2.Body.Close()
	var issues []interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&issues); err != nil {
		return RepoStats{}, err
	}
	goodFirstIssues := len(issues)

	// Security issues (simplified: count open issues with 'security' label)
	secURL := fmt.Sprintf("https://api.github.com/repos/%s/issues?labels=security&state=open", ownerRepo)
	req3, _ := http.NewRequest("GET", secURL, nil)
	if token != "" {
		req3.Header.Set("Authorization", "token "+token)
	}
	resp3, err := client.Do(req3)
	if err != nil {
		return RepoStats{}, err
	}
	defer resp3.Body.Close()
	var secIssues []interface{}
	if err := json.NewDecoder(resp3.Body).Decode(&secIssues); err != nil {
		return RepoStats{}, err
	}
	securityIssues := len(secIssues)

	// Recent commits (last 30 days)
	commitsURL := fmt.Sprintf("https://api.github.com/repos/%s/commits?since=%s", ownerRepo, time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	req4, _ := http.NewRequest("GET", commitsURL, nil)
	if token != "" {
		req4.Header.Set("Authorization", "token "+token)
	}
	resp4, err := client.Do(req4)
	if err != nil {
		return RepoStats{}, err
	}
	defer resp4.Body.Close()
	var commits []struct {
		Commit struct {
			Author struct {
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp4.Body).Decode(&commits); err != nil {
		return RepoStats{}, err
	}
	recentCommits := len(commits)
	lastCommit := "N/A"
	if len(commits) > 0 {
		lastCommit = commits[0].Commit.Author.Date
	}

	return RepoStats{
		Name:            data.Name,
		URL:             repoURL,
		Stars:           data.StargazersCount,
		Forks:           data.ForksCount,
		OpenIssues:      data.OpenIssuesCount,
		GoodFirstIssues: goodFirstIssues,
		SecurityIssues:  securityIssues,
		RecentCommits:   recentCommits,
		LastCommit:      lastCommit,
	}, nil
}

func calcXP(stats RepoStats) int {
	return stats.Stars*10 + stats.Forks*5 + stats.RecentCommits*20 - stats.OpenIssues*2
}

func calcLevel(xp int) int {
	switch {
	case xp < 100:
		return 1
	case xp < 300:
		return 2
	case xp < 700:
		return 3
	case xp < 1500:
		return 4
	default:
		return 5
	}
}

func assignBadges(stats RepoStats, mostStars int) []string {
	badges := []string{}
	if stats.Stars == mostStars {
		badges = append(badges, "🏆 Hot Project")
	}
	if stats.RecentCommits >= 5 {
		badges = append(badges, "🚀 Active Development")
	}
	if stats.GoodFirstIssues > 0 {
		badges = append(badges, "🧑‍💻 Welcoming Issues")
	}
	if stats.SecurityIssues == 0 {
		badges = append(badges, "🛡️ Secure")
	}
	return badges
}

func formatMarkdown(repos []GhProjects) string {
	var b strings.Builder
	for _, r := range repos {
		badges := strings.Join(r.Badges, ", ")
		b.WriteString(fmt.Sprintf("* [%s](%s)  \n  XP: %d | Level: %d %s  \n  Badges: %s  \n  Last Commit: %s | Stars: %d | Forks: %d | Open Issues: %d\n",
			r.Name, r.URL, r.XP, r.Level, badges, badges, r.LastCommit, r.Stars, r.Forks, r.OpenIssues))
	}
	return b.String()
}

func main() {
	org := "HappyHackingSpace"
	token := os.Getenv("GITHUB_TOKEN")

	repoURLs, err := fetchOrgRepos(org, token)
	if err != nil {
		panic(err)
	}

	var statsList []RepoStats
	mostStars := 0
	for _, repoURL := range repoURLs {
		stats, err := fetchRepoStats(strings.TrimPrefix(repoURL, "https://github.com/"), token)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching %s: %v\n", repoURL, err)
			continue
		}
		statsList = append(statsList, stats)
		if stats.Stars > mostStars {
			mostStars = stats.Stars
		}
	}

	var projects []GhProjects
	for _, s := range statsList {
		xp := calcXP(s)
		level := calcLevel(xp)
		badges := assignBadges(s, mostStars)
		projects = append(projects, GhProjects{RepoStats: s, XP: xp, Level: level, Badges: badges})
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].XP > projects[j].XP })

	md := formatMarkdown(projects)

	readmePath := "profile/README.md"
	readme, err := ioutil.ReadFile(readmePath)
	if err != nil {
		panic(err)
	}
	re := regexp.MustCompile(`(?s)<!-- PROJECTS_START -->(.*?)<!-- PROJECTS_END -->`)
	newReadme := re.ReplaceAll(readme, []byte("<!-- PROJECTS_START -->\n"+md+"<!-- PROJECTS_END -->"))
	if err := ioutil.WriteFile(readmePath, newReadme, 0644); err != nil {
		panic(err)
	}
}
