package main

import (
	context "context"
	"fmt"
	"os"
	"regexp"
	"slices"
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

type ContributorStats struct {
	Login      string
	ProfileURL string
	AvatarURL  string
	Commits    int
	Issues     int
	PRs        int
	XP         int
	Level      int
	Badges     []string
}

var excludedProjects = []string{".github", "CTFd"}

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
		b.WriteString(fmt.Sprintf("* [%s](%s)  \n  XP: %d | Level: %d  \n  Badges: %s  \n  Last Commit: %s | Stars: %d | Forks: %d | Open Issues: %d\n",
			r.Name, r.URL, r.XP, r.Level, badges, r.LastCommit, r.Stars, r.Forks, r.OpenIssues))
	}
	return b.String()
}

func fetchContributors(client *gh.Client, ctx context.Context, org string, repos []*gh.Repository) []ContributorStats {
	excludedContributors := []string{"dependabot[bot]"}
	contribMap := map[string]*ContributorStats{}
	for _, repo := range repos {
		if slices.Contains(excludedProjects, repo.GetName()) {
			continue
		}

		// Commits
		commitOpt := &gh.CommitsListOptions{ListOptions: gh.ListOptions{PerPage: 100}}
		for {
			commits, resp, err := client.Repositories.ListCommits(ctx, org, repo.GetName(), commitOpt)
			if err != nil {
				break
			}
			for _, c := range commits {
				if c.Author == nil {
					continue
				}
				login := c.Author.GetLogin()
				if login == "" || slices.Contains(excludedContributors, login) {
					continue
				}
				if _, ok := contribMap[login]; !ok {
					contribMap[login] = &ContributorStats{
						Login:      login,
						ProfileURL: c.Author.GetHTMLURL(),
						AvatarURL:  c.Author.GetAvatarURL(),
					}
				}
				contribMap[login].Commits++
			}
			if resp.NextPage == 0 {
				break
			}
			commitOpt.Page = resp.NextPage
		}
		// Issues
		issueOpt := &gh.IssueListByRepoOptions{State: "all", ListOptions: gh.ListOptions{PerPage: 100}}
		for {
			issues, resp, err := client.Issues.ListByRepo(ctx, org, repo.GetName(), issueOpt)
			if err != nil {
				break
			}
			for _, issue := range issues {
				if issue.User == nil || issue.PullRequestLinks != nil {
					continue // skip PRs here
				}
				login := issue.User.GetLogin()
				if login == "" || slices.Contains(excludedContributors, login) {
					continue
				}
				if _, ok := contribMap[login]; !ok {
					contribMap[login] = &ContributorStats{
						Login:      login,
						ProfileURL: issue.User.GetHTMLURL(),
						AvatarURL:  issue.User.GetAvatarURL(),
					}
				}
				contribMap[login].Issues++
			}
			if resp.NextPage == 0 {
				break
			}
			issueOpt.ListOptions.Page = resp.NextPage
		}
		// PRs
		prOpt := &gh.PullRequestListOptions{State: "all", ListOptions: gh.ListOptions{PerPage: 100}}
		for {
			prs, resp, err := client.PullRequests.List(ctx, org, repo.GetName(), prOpt)
			if err != nil {
				break
			}
			for _, pr := range prs {
				if pr.User == nil {
					continue
				}
				login := pr.User.GetLogin()
				if login == "" || slices.Contains(excludedContributors, login) {
					continue
				}
				if _, ok := contribMap[login]; !ok {
					contribMap[login] = &ContributorStats{
						Login:      login,
						ProfileURL: pr.User.GetHTMLURL(),
						AvatarURL:  pr.User.GetAvatarURL(),
					}
				}
				contribMap[login].PRs++
			}
			if resp.NextPage == 0 {
				break
			}
			prOpt.Page = resp.NextPage
		}
	}
	// Calculate XP, Level, Badges
	var contribs []ContributorStats
	mostCommits := 0
	for _, c := range contribMap {
		c.XP = c.Commits*5 + c.Issues*2 + c.PRs*3
		c.Level = calcLevel(c.XP)
		if c.Commits > mostCommits {
			mostCommits = c.Commits
		}
		contribs = append(contribs, *c)
	}
	for i := range contribs {
		badges := []string{}
		if contribs[i].Commits == mostCommits {
			badges = append(badges, "🏆 Top Committer")
		}
		if contribs[i].PRs > 0 {
			badges = append(badges, "🔀 PR Hero")
		}
		if contribs[i].Issues > 0 {
			badges = append(badges, "🐛 Issue Opener")
		}
		contribs[i].Badges = badges
	}
	sort.Slice(contribs, func(i, j int) bool { return contribs[i].XP > contribs[j].XP })
	return contribs
}

func formatContributorsMarkdown(contribs []ContributorStats) string {
	var b strings.Builder
	for _, c := range contribs {
		badges := strings.Join(c.Badges, ", ")
		b.WriteString(fmt.Sprintf("* [%s](%s)  \n  XP: %d | Level: %d  \n  Badges: %s  \n  Commits: %d | Issues: %d | PRs: %d\n",
			c.Login, c.ProfileURL, c.XP, c.Level, badges, c.Commits, c.Issues, c.PRs))
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
		if slices.Contains(excludedProjects, repo.GetName()) {
			continue
		}
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

	// Only show the first 10 projects
	displayCount := min(len(projects), 10)
	md := formatMarkdown(projects[:displayCount])
	md += "\n[...and more projects](https://github.com/HappyHackingSpace?tab=repositories)"

	// Contributors section
	contributors := fetchContributors(client, ctx, org, repos)
	contribDisplayCount := min(len(contributors), 10)
	contribMd := formatContributorsMarkdown(contributors[:contribDisplayCount])
	contribMd += "\n[...and more contributors](https://github.com/orgs/HappyHackingSpace/people)"

	readmePath := "profile/README.md"
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		panic(err)
	}
	re := regexp.MustCompile(`(?s)<!-- PROJECTS_START -->(.*?)<!-- PROJECTS_END -->`)
	newReadme := re.ReplaceAll(readme, []byte("<!-- PROJECTS_START -->\n"+md+"<!-- PROJECTS_END -->"))

	re2 := regexp.MustCompile(`(?s)<!-- CONTRIBUTORS_START -->(.*?)<!-- CONTRIBUTORS_END -->`)
	newReadme = re2.ReplaceAll(newReadme, []byte("<!-- CONTRIBUTORS_START -->\n"+contribMd+"<!-- CONTRIBUTORS_END -->"))

	if err := os.WriteFile(readmePath, newReadme, 0644); err != nil {
		panic(err)
	}
}
