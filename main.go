package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"gator/internal/config"
	"gator/internal/database"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type Commands struct {
	Handlers map[string]func(*config.State, config.Command) error
}

type RSSFeed struct {
	Channel struct {
		Title       string    `xml:"title"`
		Link        string    `xml:"link"`
		Description string    `xml:"description"`
		Item        []RSSItem `xml:"item"`
	} `xml:"channel"`
}

type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func handlerBrowse(state *config.State, cmd config.Command) error {
	limit := 2
	if len(cmd.Args) > 0 {
		if parsedLimit, err := strconv.Atoi(cmd.Args[0]); err == nil {
			limit = parsedLimit
		} else {
			return fmt.Errorf("Error: %s", cmd.Args[0])
		}
	}
	posts, err := state.DB.GetPosts(context.Background(), int32(limit))
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	for _, post := range posts {
		fmt.Printf("- %s (%s)\n", post.Title, post.Url)
	}
	return nil
}

func parseTime(dateStr string) (time.Time, error) {
	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC3339,
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, dateStr)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("Error: %s", dateStr)
}

func scrapeFeeds(s *config.State) error {
	ctx := context.Background()
	feed, err := s.DB.GetNextFeedToFetch(ctx)
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	err = s.DB.MarkFeedFetched(ctx, feed.ID)
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	rssFeed, err := fetchFeed(ctx, feed.Url)
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	for _, item := range rssFeed.Channel.Item {
		publishedAt, err := parseTime(item.PubDate)
		if err != nil {
			log.Printf("Error: %s: %v", item.Title, err)
			continue
		}
		err = s.DB.CreatePost(ctx, database.CreatePostParams{
			Title:       item.Title,
			Url:         item.Link,
			Description: sql.NullString{String: item.Description, Valid: true},
			PublishedAt: sql.NullTime{Time: publishedAt, Valid: true},
			FeedID:      feed.ID,
		})
		if err != nil {
			log.Printf("Error: %v", err)
		}
	}
	return nil
}


func handlerUnfollowFeed(s *config.State, cmd config.Command, user database.User) error {
	if len(cmd.Args) < 1 {
		return fmt.Errorf("Error: need more arguments.")
	}
	feedURL := cmd.Args[0]
	feed, err := s.DB.GetFeedByUrl(context.Background(), feedURL)
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	err = s.DB.DeleteFeedFollow(context.Background(), database.DeleteFeedFollowParams{
		UserID: user.ID,
		FeedID: feed.ID,
	})
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	fmt.Printf("Successfully unfollowed the feed: %s\n", feedURL)
	return nil
}

func middlewareLoggedIn(handler func(s *config.State, cmd config.Command, user database.User) error) func(*config.State, config.Command) error {
	return func(s *config.State, cmd config.Command) error {
		if s.Config.CurrentUserName == "" {
			return fmt.Errorf("Error: No user logged in.")
		}
		user, err := s.DB.GetUser(context.Background(), s.Config.CurrentUserName)
		if err != nil {
			return fmt.Errorf("Error: %w", err)
		}
		return handler(s, cmd, user)
	}
}

func handlerListFeedFollows(s *config.State, cmd config.Command, user database.User) error {
	follows, err := s.DB.GetFeedFollowsForUser(context.Background(), user.ID)
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	if len(follows) == 0 {
		fmt.Println("No feed follows found.")
		return nil
	}
	fmt.Println("Your followed feeds:")
	for _, follow := range follows {
		fmt.Printf(" - %s (%s)\n", follow.FeedName, follow.FeedUrl)
	}
	return nil
}

func handlerFollowFeed(s *config.State, cmd config.Command, user database.User) error {
	if len(cmd.Args) < 1 {
		return fmt.Errorf("Error: Need more arguments.")
	}
	feedURL := cmd.Args[0]
	feed, err := s.DB.GetFeedByUrl(context.Background(), feedURL)
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}

	_, err = s.DB.CreateFeedFollow(context.Background(), database.CreateFeedFollowParams{
		UserID: user.ID,
		FeedID: feed.ID,
	})
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}

	fmt.Printf("Successfully followed the feed!\nFeed: %s\nUser: %s\n", feed.Name, user.Name)
	return nil
}

func handlerAddFeed(s *config.State, cmd config.Command, user database.User) error {
	if len(cmd.Args) < 2 {
		return fmt.Errorf("Error: Need more arguments.")
	}
	name := cmd.Args[0]
	url := cmd.Args[1]
	feed, err := s.DB.CreateFeed(context.Background(), database.CreateFeedParams{
		Name:   name,
		Url:    url,
		UserID: user.ID,
	})
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	_, err = s.DB.CreateFeedFollow(context.Background(), database.CreateFeedFollowParams{
		UserID: user.ID,
		FeedID: feed.ID,
	})
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	fmt.Printf("Feed added and followed successfully!\nID: %d\nName: %s\nURL: %s\nUser: %s\n", feed.ID, feed.Name, feed.Url, user.Name)
	return nil
}

func handlerFeeds(s *config.State, cmd config.Command) error {
    feeds, err := s.DB.GetFeedsWithUsers(context.Background())
    if err != nil {
        return fmt.Errorf("Error: %w", err)
    }
    if len(feeds) == 0 {
        fmt.Println("No feeds found.")
        return nil
    }
    for _, feed := range feeds {
        fmt.Printf("Name: %s\n", feed.Name)
        fmt.Printf("URL: %s\n", feed.Url)
        fmt.Printf("Added by: %s\n", feed.UserName)
    }
    return nil
}

func handlerAgg(s *config.State, cmd config.Command) error {
	if len(cmd.Args) < 1 {
		return fmt.Errorf("Error: Need more arguments.")
	}
	durationStr := cmd.Args[0]
	timeBetweenRequests, err := time.ParseDuration(durationStr)
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	fmt.Printf("Collecting feeds every %s\n", timeBetweenRequests)
	ticker := time.NewTicker(timeBetweenRequests)
	defer ticker.Stop()
	scrapeFeeds(s)
	for {
		select {
		case <-ticker.C:
			err := scrapeFeeds(s)
			if err != nil {
				log.Printf("error scraping feeds: %v", err)
			}
		}
	}
}


func fetchFeed(ctx context.Context, feedURL string) (*RSSFeed, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("Error: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Error: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Error: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Error: %w", err)
	}
	var feed RSSFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("Error: %w", err)
	}
	feed.Channel.Title = html.UnescapeString(feed.Channel.Title)
	feed.Channel.Description = html.UnescapeString(feed.Channel.Description)
	for i, item := range feed.Channel.Item {
		feed.Channel.Item[i].Title = html.UnescapeString(item.Title)
		feed.Channel.Item[i].Description = html.UnescapeString(item.Description)
	}
	return &feed, nil
}

func handlerReset(s *config.State, cmd config.Command) error {
	ctx := context.Background()
	err := s.DB.ResetUsers(ctx)
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	fmt.Println("Database successfully reset.")
	return nil
}

func handlerRegister(s *config.State, cmd config.Command) error {
	if len(cmd.Args) == 0 {
		return fmt.Errorf("Error: Need more arguments.")
	}
	username := cmd.Args[0]
	_, err := s.DB.GetUser(context.Background(), username)
	if err == nil {
		return fmt.Errorf("Error: User %s already exists.", username)
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("Error: %w", err)
	}
	userID := uuid.New()
	now := time.Now()
	newUser, err := s.DB.CreateUser(context.Background(), database.CreateUserParams{
		ID:        userID,
		CreatedAt: sql.NullTime{Time: now, Valid: true},
		UpdatedAt: sql.NullTime{Time: now, Valid: true},
		Name:      username,
	})
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	s.Config.CurrentUserName = newUser.Name
	if err := saveConfig(s.Config); err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	fmt.Printf("User '%s' successfully registered!\n", newUser.Name)
	fmt.Printf("User details: %v\n", newUser)
	return nil
}

func handlerLogin(s *config.State, cmd config.Command) error {
	if len(cmd.Args) == 0 {
		return fmt.Errorf("Error: Need more arguments.")
	}
	username := cmd.Args[0]
	user, err := s.DB.GetUser(context.Background(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("Error: User '%s' does not exist.", username)
		}
		return fmt.Errorf("Error: %w", err)
	}
	s.Config.CurrentUserName = user.Name
	err = saveConfig(s.Config)
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	fmt.Printf("User '%s' successfully logged in.\n", user.Name)
	return nil
}

func saveConfig(cfg *config.Config) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	configFilePath := fmt.Sprintf("%s/.gatorconfig.json", homeDir)
	file, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(cfg)
}

func main() {
	cfg, err := config.Read()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	db, err := sql.Open("postgres", cfg.DBURL)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	dbQueries := database.New(db)
	state := &config.State{Config: cfg, DB: dbQueries}
	commands := &Commands{}
	commands.Register("login", handlerLogin)
	commands.Register("register", handlerRegister)
	commands.Register("reset", handlerReset)
	commands.Register("users", handlerUsers)
	commands.Register("agg", handlerAgg)
	commands.Register("feeds", handlerFeeds)
	commands.Register("addfeed", middlewareLoggedIn(handlerAddFeed))
	commands.Register("follow", middlewareLoggedIn(handlerFollowFeed))
	commands.Register("following", middlewareLoggedIn(handlerListFeedFollows))
	commands.Register("unfollow", middlewareLoggedIn(handlerUnfollowFeed))
	commands.Register("browse", handlerBrowse)
	if len(os.Args) < 2 {
		log.Fatalf("Error: Need more arguments.")
	}
	cmdName := os.Args[1]
	args := os.Args[2:]
	cmd := config.Command{Name: cmdName, Args: args}
	if err := commands.Run(state, cmd); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func (c *Commands) Register(name string, f func(*config.State, config.Command) error) {
	if c.Handlers == nil {
		c.Handlers = make(map[string]func(*config.State, config.Command) error)
	}
	c.Handlers[name] = f
}

func (c *Commands) Run(s *config.State, cmd config.Command) error {
	if handler, found := c.Handlers[cmd.Name]; found {
		return handler(s, cmd)
	}
	return fmt.Errorf("Unknown command: %s", cmd.Name)
}

func handlerUsers(s *config.State, cmd config.Command) error {
	users, err := s.DB.GetUsers(context.Background())
	if err != nil {
		return fmt.Errorf("Error: %w", err)
	}
	currentUser := s.Config.CurrentUserName
	for _, user := range users {
		if user.Name == currentUser {
			fmt.Printf("* %s (current)\n", user.Name)
		} else {
			fmt.Printf("* %s\n", user.Name)
		}
	}
	return nil
}
