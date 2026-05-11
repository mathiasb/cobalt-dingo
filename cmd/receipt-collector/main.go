package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/mathiasb/coo-agent/internal/receipts"
	"gopkg.in/yaml.v3"
)

const configPath = "config/receipt-sources.yml"

type configFile struct {
	Sources      []sourceConfig      `yaml:"sources"`
	Destinations []destinationConfig `yaml:"destinations"`
	Rules        []ruleConfig        `yaml:"rules"`
}

type sourceConfig struct {
	Name        string `yaml:"name"`
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	Username    string `yaml:"username"`
	PasswordEnv string `yaml:"password_env"`
	Folder      string `yaml:"folder"`
	TLS         bool   `yaml:"tls"`
}

type destinationConfig struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Address string `yaml:"address"`
}

type ruleConfig struct {
	Name         string   `yaml:"name"`
	MatchFrom    []string `yaml:"match_from"`
	MatchSubject []string `yaml:"match_subject"`
	Destination  string   `yaml:"destination"`
}

func main() {
	_ = godotenv.Load()

	dryRun := true
	if v := os.Getenv("DRY_RUN"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			dryRun = b
		}
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("fel vid läsning av %s: %v", configPath, err)
	}

	sources := make([]*receipts.Source, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		password := os.Getenv(s.PasswordEnv)
		if password == "" {
			log.Fatalf("miljövariabel %s är inte satt (source: %s)", s.PasswordEnv, s.Name)
		}
		sources = append(sources, &receipts.Source{
			Name:     s.Name,
			Host:     s.Host,
			Port:     s.Port,
			Username: s.Username,
			Password: password,
			Folder:   s.Folder,
			TLS:      s.TLS,
		})
	}

	dests := make([]*receipts.Destination, 0, len(cfg.Destinations))
	for _, d := range cfg.Destinations {
		dests = append(dests, &receipts.Destination{
			Name:    d.Name,
			Type:    d.Type,
			Address: d.Address,
		})
	}

	rules := make([]receipts.Rule, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		rules = append(rules, receipts.Rule{
			Name:         r.Name,
			MatchFrom:    r.MatchFrom,
			MatchSubject: r.MatchSubject,
			Destination:  r.Destination,
		})
	}

	router := receipts.NewRouter(rules, dests)
	collector := receipts.NewCollector(sources, router, dryRun)

	if dryRun {
		fmt.Println("=== DRY RUN – inga mail vidarebefordras ===")
	}

	results, err := collector.Run(context.Background())
	if err != nil {
		log.Fatalf("insamlingsfel: %v", err)
	}

	exitCode := 0
	for _, r := range results {
		fmt.Printf("\nKonto: %s\n", r.Account)
		fmt.Printf("  Hämtade:   %d\n", r.Processed)
		fmt.Printf("  Routade:   %d\n", r.Routed)
		fmt.Printf("  Omatchade: %d\n", r.UnmatchedCount)
		if len(r.Errors) > 0 {
			exitCode = 1
			fmt.Printf("  Fel:       %d\n", len(r.Errors))
			for _, e := range r.Errors {
				fmt.Printf("    - %v\n", e)
			}
		}
	}

	os.Exit(exitCode)
}

func loadConfig(path string) (*configFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("öppna %s: %w", path, err)
	}
	defer f.Close()

	var cfg configFile
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("tolka yaml: %w", err)
	}
	return &cfg, nil
}
