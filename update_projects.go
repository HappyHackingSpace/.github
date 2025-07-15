package main

import (
	context "context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	gh "github.com/google/go-github/v73/github"
	"golang.org/x/oauth2"
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

func fetchOrgRepos(client *gh.Client, ctx context.Context, org string) ([]*gh.Repository, error) {
	var allRepos []*gh.Repository
	opt := &gh.RepositoryListByOrgOptions{Type: "public", ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		repos, resp, err := client.Repositories.ListByOrg(ctx, org, opt)
		if err != nil {
			return nil, err
		}
		allRepos = append(allRepos, repos...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return allRepos, nil
}

func fetchRepoStats(client *gh.Client, ctx context.Context, org string, repo *gh.Repository) (RepoStats, error) {
	name := repo.GetName()
	url := repo.GetHTMLURL()
	stars := repo.GetStargazersCount()
	forks := repo.GetForksCount()
	openIssues := repo.GetOpenIssuesCount()

	// Good first issues
	goodFirstIssues := 0
	issueOpt := &gh.IssueListByRepoOptions{
		State:       "open",
		Labels:      []string{"good first issue"},
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	for {
		issues, resp, err := client.Issues.ListByRepo(ctx, org, name, issueOpt)
		if err != nil {
			return RepoStats{}, err
		}
		goodFirstIssues += len(issues)
		if resp.NextPage == 0 {
			break
		}
		issueOpt.ListOptions.Page = resp.NextPage
	}

	// Security issues
	securityIssues := 0
	secOpt := &gh.IssueListByRepoOptions{
		State:       "open",
		Labels:      []string{"security"},
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	for {
		issues, resp, err := client.Issues.ListByRepo(ctx, org, name, secOpt)
		if err != nil {
			return RepoStats{}, err
		}
		securityIssues += len(issues)
		if resp.NextPage == 0 {
			break
		}
		secOpt.ListOptions.Page = resp.NextPage
	}

	// Recent commits (last 30 days)
	since := time.Now().AddDate(0, 0, -30)
	commitOpt := &gh.CommitsListOptions{
		Since:       since,
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	recentCommits := 0
	var lastCommit string = "N/A"
	for {
		commits, resp, err := client.Repositories.ListCommits(ctx, org, name, commitOpt)
		if err != nil {
			return RepoStats{}, err
		}
		recentCommits += len(commits)
		if len(commits) > 0 && lastCommit == "N/A" {
			lastCommit = commits[0].GetCommit().GetAuthor().GetDate().Format(time.RFC3339)
		}
		if resp.NextPage == 0 {
			break
		}
		commitOpt.Page = resp.NextPage
	}

	return RepoStats{
		Name:            name,
		URL:             url,
		Stars:           stars,
		Forks:           forks,
		OpenIssues:      openIssues,
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
	ctx := context.Background()
	var client *gh.Client
	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		client = gh.NewClient(oauth2.NewClient(ctx, ts))
	} else {
		client = gh.NewClient(nil)
	}

	repos, err := fetchOrgRepos(client, ctx, org)
	if err != nil {
		panic(err)
	}

	var statsList []RepoStats
	mostStars := 0
	for _, repo := range repos {
		stats, err := fetchRepoStats(client, ctx, org, repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching %s: %v\n", repo.GetHTMLURL(), err)
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
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		panic(err)
	}
	re := regexp.MustCompile(`(?s)<!-- PROJECTS_START -->(.*?)<!-- PROJECTS_END -->`)
	newReadme := re.ReplaceAll(readme, []byte("<!-- PROJECTS_START -->\n"+md+"<!-- PROJECTS_END -->"))
	if err := os.WriteFile(readmePath, newReadme, 0644); err != nil {
		panic(err)
	}
}
