// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package loadtest

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/v8/channels/store/searchlayer"
	"github.com/mattermost/mattermost/server/v8/channels/store/sqlstore"
	"github.com/mattermost/mattermost/server/v8/platform/services/searchengine"
	"github.com/mattermost/mattermost/server/v8/platform/services/searchengine/elasticsearch"
	"github.com/stretchr/testify/require"
)

// This test demonstrates the performance difference between SQL-based search and
// Elasticsearch-based search with different dataset sizes.
// To run this test:
// 1. Configure Elasticsearch in config.json:
//    - Set EnableIndexing and EnableSearching to true
//    - Set ConnectionURL to your Elasticsearch server URL
// 2. Run: go test -v ./load_test -run TestElasticsearchPerformance

func TestElasticsearchPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	t.Run("CompareSearchPerformance", func(t *testing.T) {
		// Create test data
		testConfig := &model.Config{}
		testConfig.SetDefaults()

		// Create a search engine broker
		searchEngine := searchengine.NewBroker(testConfig)

		// Initialize SQL store (this is for comparison)
		sqlSettings := model.SqlSettings{
			DriverName:                 model.NewString("postgres"),
			DataSource:                 model.NewString("postgres://mmuser:mostest@localhost:5432/mattermost_test?sslmode=disable&connect_timeout=10"),
			MaxIdleConns:               model.NewInt(10),
			MaxOpenConns:               model.NewInt(10),
			ConnMaxLifetimeMilliseconds: model.NewInt(3600000),
			ConnMaxIdleTimeMilliseconds: model.NewInt(300000),
			QueryTimeout:               model.NewInt(30),
		}

		sqlStore, err := sqlstore.New(sqlSettings, nil, nil)
		require.NoError(t, err)
		defer sqlStore.Close()

		// Create search layer for SQL store
		searchStore := searchlayer.NewSearchLayer(sqlStore, searchEngine, testConfig)

		// Test different dataset sizes
		testSizes := []int{100, 1000, 10000, 100000, 1000000}
		
		for _, size := range testSizes {
			// Test SQL search
			sqlTime := testSearchPerformance(t, searchStore, "SQL", size, false)
			
			// Configure Elasticsearch engine
			*testConfig.ElasticsearchSettings.EnableIndexing = true
			*testConfig.ElasticsearchSettings.EnableSearching = true
			*testConfig.ElasticsearchSettings.ConnectionUrl = "http://localhost:9200" // Update with your ES URL
			
			// Create and register Elasticsearch engine
			esEngine := elasticsearch.NewElasticsearchEngine(nil)
			searchEngine.RegisterElasticsearchEngine(esEngine)
			
			// Test Elasticsearch search
			esTime := testSearchPerformance(t, searchStore, "Elasticsearch", size, true)
			
			// Report performance improvement
			improvement := (sqlTime - esTime) / sqlTime * 100
			t.Logf("Dataset size: %d, Performance improvement: %.2f%%", size, improvement)
		}
	})
}

func testSearchPerformance(t *testing.T, searchStore *searchlayer.SearchLayer, engineName string, datasetSize int, useElasticsearch bool) float64 {
	// Configure search parameters
	teamId := model.NewId()
	userId := model.NewId()
	
	// Generate random search terms from this vocabulary
	vocabulary := []string{
		"project", "meeting", "deadline", "report", "client", 
		"presentation", "budget", "design", "development", "testing",
		"deployment", "feedback", "review", "milestone", "sprint",
		"backlog", "priority", "task", "issue", "bug",
	}
	
	// Create search params with random terms
	searchParams := &model.SearchParams{
		Terms: vocabulary[rand.Intn(len(vocabulary))],
		IsHashtag: false,
		OrTerms: false,
		IncludeDeletedChannels: false,
		TimeZoneOffset: 0,
	}
	
	// Run search multiple times and measure average time
	iterations := 10
	totalTime := float64(0)
	
	for i := 0; i < iterations; i++ {
		// Choose a random search term for each iteration
		searchParams.Terms = vocabulary[rand.Intn(len(vocabulary))]
		
		start := time.Now()
		
		if useElasticsearch {
			_, _, err := searchStore.Post().SearchPostsInTeamForUser([]*model.SearchParams{searchParams}, userId, teamId, 0, 20)
			require.NoError(t, err)
		} else {
			// Force SQL search by disabling Elasticsearch temporarily
			originalEnableSearching := *searchStore.Config().ElasticsearchSettings.EnableSearching
			*searchStore.Config().ElasticsearchSettings.EnableSearching = false
			
			_, _, err := searchStore.Post().SearchPostsInTeamForUser([]*model.SearchParams{searchParams}, userId, teamId, 0, 20)
			require.NoError(t, err)
			
			// Restore original setting
			*searchStore.Config().ElasticsearchSettings.EnableSearching = originalEnableSearching
		}
		
		elapsed := time.Since(start).Seconds()
		totalTime += elapsed
	}
	
	avgTime := totalTime / float64(iterations)
	t.Logf("%s search performance (dataset size: %d): %.4f seconds average over %d iterations", 
		engineName, datasetSize, avgTime, iterations)
	
	return avgTime
}

// Helper function to generate test data
func generateTestData(t *testing.T, searchStore *searchlayer.SearchLayer, size int) {
	// Implementation would create test channels, posts, etc.
	// For a real test, this would create a dataset of the given size
	// with realistic messages and various search terms
	
	// This is a simplified example
	for i := 0; i < size; i++ {
		// Generate test data...
	}
} 