// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package elasticsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/public/shared/request"
	"github.com/mattermost/mattermost/server/v8/channels/app/platform"
	"github.com/mattermost/mattermost/server/v8/platform/services/searchengine"

	elastic "github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/deletebyquery"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/highlighterencoder"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/operator"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/sortorder"
)

const (
	EngineVersion     = 1
	IndexBasePosts    = "posts"
	IndexBaseChannels = "channels"
	IndexBaseUsers    = "users"
)

type ElasticsearchEngine struct {
	client      *elastic.TypedClient
	mutex       sync.RWMutex
	ready       int32
	version     int
	fullVersion string
	plugins     []string
	Platform    *platform.PlatformService
}

func NewElasticsearchEngine(ps *platform.PlatformService) *ElasticsearchEngine {
	return &ElasticsearchEngine{
		Platform: ps,
		version:  EngineVersion,
	}
}

func getJSONOrErrorStr(obj any) string {
	b, err := json.Marshal(obj)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func (es *ElasticsearchEngine) UpdateConfig(cfg *model.Config) {
	// Not needed, it uses the Platform stored internally to get always the last version
}

func (es *ElasticsearchEngine) GetName() string {
	return "elasticsearch"
}

func (es *ElasticsearchEngine) IsEnabled() bool {
	return *es.Platform.Config().ElasticsearchSettings.EnableIndexing
}

func (es *ElasticsearchEngine) IsActive() bool {
	return *es.Platform.Config().ElasticsearchSettings.EnableIndexing && atomic.LoadInt32(&es.ready) == 1
}

func (es *ElasticsearchEngine) IsIndexingEnabled() bool {
	return *es.Platform.Config().ElasticsearchSettings.EnableIndexing
}

func (es *ElasticsearchEngine) IsSearchEnabled() bool {
	return *es.Platform.Config().ElasticsearchSettings.EnableSearching
}

func (es *ElasticsearchEngine) IsAutocompletionEnabled() bool {
	return *es.Platform.Config().ElasticsearchSettings.EnableAutocomplete
}

func (es *ElasticsearchEngine) IsIndexingSync() bool {
	return *es.Platform.Config().ElasticsearchSettings.LiveIndexingBatchSize <= 1
}

func (es *ElasticsearchEngine) Start() *model.AppError {
	es.mutex.Lock()
	defer es.mutex.Unlock()

	if !*es.Platform.Config().ElasticsearchSettings.EnableIndexing {
		return nil
	}

	if atomic.LoadInt32(&es.ready) == 1 {
		return nil
	}

	cfg := elastic.Config{
		Addresses: []string{*es.Platform.Config().ElasticsearchSettings.ConnectionUrl},
		Username:  *es.Platform.Config().ElasticsearchSettings.Username,
		Password:  *es.Platform.Config().ElasticsearchSettings.Password,
	}

	client, err := elastic.NewTypedClient(cfg)
	if err != nil {
		return model.NewAppError("elasticsearch.Start", "searchengine.elasticsearch.start.error", nil, "", http.StatusInternalServerError).Wrap(err)
	}

	es.client = client

	info, err := client.API.Info().Do(context.Background())
	if err != nil {
		return model.NewAppError("elasticsearch.Start", "searchengine.elasticsearch.info.error", nil, "", http.StatusInternalServerError).Wrap(err)
	}

	es.fullVersion = info.Version.Number
	
	// Set up index templates for posts, channels, users, and files
	ctx := context.Background()
	
	// Posts index template
	postsTemplate := map[string]interface{}{
		"index_patterns": []string{*es.Platform.Config().ElasticsearchSettings.IndexPrefix + "*_posts*"},
		"settings": map[string]interface{}{
			"number_of_shards": 1,
			"number_of_replicas": 0,
		},
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type": "text",
				},
				"team_id": map[string]interface{}{
					"type": "keyword",
				},
				"channel_id": map[string]interface{}{
					"type": "keyword",
				},
				"user_id": map[string]interface{}{
					"type": "keyword",
				},
				"create_at": map[string]interface{}{
					"type": "date",
				},
				"type": map[string]interface{}{
					"type": "keyword",
				},
				"hashtags": map[string]interface{}{
					"type": "keyword",
				},
			},
		},
	}
	
	_, err = es.client.API.Indices.PutIndexTemplate(*es.Platform.Config().ElasticsearchSettings.IndexPrefix + "posts").
		Request(postsTemplate).
		Do(ctx)
	if err != nil {
		return model.NewAppError("elasticsearch.Start", "searchengine.elasticsearch.create_posts_index_template.error", nil, "", http.StatusInternalServerError).Wrap(err)
	}
	
	atomic.StoreInt32(&es.ready, 1)

	return nil
}

func (es *ElasticsearchEngine) Stop() *model.AppError {
	es.mutex.Lock()
	defer es.mutex.Unlock()

	if atomic.LoadInt32(&es.ready) == 0 {
		return nil
	}

	atomic.StoreInt32(&es.ready, 0)
	return nil
}

func (es *ElasticsearchEngine) GetVersion() int {
	return es.version
}

func (es *ElasticsearchEngine) GetFullVersion() string {
	return es.fullVersion
}

func (es *ElasticsearchEngine) GetPlugins() []string {
	return es.plugins
}

func (es *ElasticsearchEngine) IndexPost(post *model.Post, teamId string) *model.AppError {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	if atomic.LoadInt32(&es.ready) == 0 {
		return model.NewAppError("ElasticsearchEngine.IndexPost", "searchengine.elasticsearch.not_started.error", nil, "", http.StatusInternalServerError)
	}

	// Convert the post to a JSON document
	jsonPost := struct {
		Id          string     `json:"id"`
		TeamId      string     `json:"team_id"`
		ChannelId   string     `json:"channel_id"`
		UserId      string     `json:"user_id"`
		CreateAt    int64      `json:"create_at"`
		Message     string     `json:"message"`
		Type        string     `json:"type"`
		Hashtags    []string   `json:"hashtags"`
		Attachments string     `json:"attachments"`
		URLs        []string   `json:"urls"`
	}{
		Id:        post.Id,
		TeamId:    teamId,
		ChannelId: post.ChannelId,
		UserId:    post.UserId,
		CreateAt:  post.CreateAt,
		Message:   post.Message,
		Type:      post.Type,
	}

	// Handle hashtags
	if len(post.Hashtags) > 0 {
		jsonPost.Hashtags = strings.Fields(post.Hashtags)
	}

	// Handle attachments
	if len(post.FileIds) > 0 || post.HasReactions {
		jsonPost.Attachments = post.Props["attachments"].(string)
	}

	// Handle URLs
	if post.Metadata != nil && len(post.Metadata.Embeds) > 0 {
		var urls []string
		for _, embed := range post.Metadata.Embeds {
			urls = append(urls, embed.URL)
		}
		jsonPost.URLs = urls
	}

	indexName := strings.ToLower(*es.Platform.Config().ElasticsearchSettings.IndexPrefix + IndexBasePosts)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*es.Platform.Config().ElasticsearchSettings.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	_, err := es.client.API.Index(indexName).ID(post.Id).Document(jsonPost).Do(ctx)
	if err != nil {
		return model.NewAppError("ElasticsearchEngine.IndexPost", "searchengine.elasticsearch.index_post.error", nil, "", http.StatusInternalServerError).Wrap(err)
	}

	return nil
}

func (es *ElasticsearchEngine) SearchPosts(channels model.ChannelList, searchParams []*model.SearchParams, page, perPage int) ([]string, model.PostSearchMatches, *model.AppError) {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	if atomic.LoadInt32(&es.ready) == 0 {
		return []string{}, nil, model.NewAppError("ElasticsearchEngine.SearchPosts", "searchengine.elasticsearch.search_posts.disabled", nil, "", http.StatusInternalServerError)
	}

	var channelIds []string
	for _, channel := range channels {
		channelIds = append(channelIds, channel.Id)
	}

	var termQueries, notTermQueries, highlightQueries []types.Query
	var filters, notFilters []types.Query

	// Add channel filter
	channelFilter := types.Query{
		Terms: &types.TermsQuery{
			Field: model.NewPointer("channel_id"),
			Terms: channelIds,
		},
	}
	filters = append(filters, channelFilter)

	// Process search parameters
	for _, params := range searchParams {
		// Handle terms
		if params.Terms != "" {
			termQuery := types.Query{
				Match: map[string]types.MatchQuery{
					"message": {
						Query:    params.Terms,
						Operator: operator.Or,
					},
				},
			}
			termQueries = append(termQueries, termQuery)
			highlightQueries = append(highlightQueries, termQuery)
		}

		// Handle excluded terms
		if params.ExcludedTerms != "" {
			excludedTermsQuery := types.Query{
				Match: map[string]types.MatchQuery{
					"message": {
						Query:    params.ExcludedTerms,
						Operator: operator.Or,
					},
				},
			}
			notTermQueries = append(notTermQueries, excludedTermsQuery)
		}

		// Handle from users
		if len(params.FromUsers) > 0 {
			fromUsersQuery := types.Query{
				Terms: &types.TermsQuery{
					Field: model.NewPointer("user_id"),
					Terms: params.FromUsers,
				},
			}
			filters = append(filters, fromUsersQuery)
		}

		// Handle excluded users
		if len(params.ExcludedUsers) > 0 {
			excludedUsersQuery := types.Query{
				Terms: &types.TermsQuery{
					Field: model.NewPointer("user_id"),
					Terms: params.ExcludedUsers,
				},
			}
			notFilters = append(notFilters, excludedUsersQuery)
		}

		// Handle in channels
		if len(params.InChannels) > 0 {
			inChannelsQuery := types.Query{
				Terms: &types.TermsQuery{
					Field: model.NewPointer("channel_id"),
					Terms: params.InChannels,
				},
			}
			filters = append(filters, inChannelsQuery)
		}

		// Handle excluded channels
		if len(params.ExcludedChannels) > 0 {
			excludedChannelsQuery := types.Query{
				Terms: &types.TermsQuery{
					Field: model.NewPointer("channel_id"),
					Terms: params.ExcludedChannels,
				},
			}
			notFilters = append(notFilters, excludedChannelsQuery)
		}

		// Handle dates
		if params.OnDate != "" {
			before, after := params.GetOnDateMillis()
			beforeFloat64 := float64(before)
			afterFloat64 := float64(after)
			onDateQ := types.Query{
				Range: map[string]types.RangeQuery{
					"create_at": {
						Gte: &afterFloat64,
						Lte: &beforeFloat64,
					},
				},
			}
			filters = append(filters, onDateQ)
		} else {
			if params.AfterDate != "" {
				minFloat := float64(params.GetAfterDateMillis())
				afterDateQ := types.Query{
					Range: map[string]types.RangeQuery{
						"create_at": {
							Gte: &minFloat,
						},
					},
				}
				filters = append(filters, afterDateQ)
			}

			if params.BeforeDate != "" {
				maxFloat := float64(params.GetBeforeDateMillis())
				beforeDateQ := types.Query{
					Range: map[string]types.RangeQuery{
						"create_at": {
							Lte: &maxFloat,
						},
					},
				}
				filters = append(filters, beforeDateQ)
			}
		}
	}

	// Build term queries
	allTermsQuery := &types.BoolQuery{
		Should: termQueries,
		MustNot: notTermQueries,
		MinimumShouldMatch: model.NewPointer(1),
	}

	fullHighlightsQuery := &types.BoolQuery{
		Should: highlightQueries,
		MinimumShouldMatch: model.NewPointer(1),
	}

	// Build complete query
	query := &types.Query{
		Bool: &types.BoolQuery{
			Filter:  append([]types.Query(nil), filters...),
			Must:    []types.Query{{Bool: allTermsQuery}},
			MustNot: append([]types.Query(nil), notFilters...),
		},
	}

	// Setup highlighting
	highlight := &types.Highlight{
		HighlightQuery: &types.Query{
			Bool: fullHighlightsQuery,
		},
		Fields: map[string]types.HighlightField{
			"message":     {},
			"attachments": {},
			"url":         {},
			"hashtag":     {},
		},
		Encoder: &highlighterencoder.Html,
	}

	// Execute the search
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*es.Platform.Config().ElasticsearchSettings.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	searchRequest := es.client.Search().
		Index(strings.ToLower(*es.Platform.Config().ElasticsearchSettings.IndexPrefix + IndexBasePosts)).
		Request(&search.Request{
			Query:     query,
			Highlight: highlight,
		}).
		Sort(types.SortOptions{SortOptions: map[string]types.FieldSort{
			"create_at": {Order: &sortorder.Desc},
		}}).
		From(page * perPage).
		Size(perPage)

	searchResult, err := searchRequest.Do(ctx)
	if err != nil {
		errorStr := "err=" + err.Error()
		return []string{}, nil, model.NewAppError("ElasticsearchEngine.SearchPosts", "searchengine.elasticsearch.search_posts.search_failed", nil, errorStr, http.StatusInternalServerError)
	}

	// Process the results
	postIds := make([]string, len(searchResult.Hits.Hits))
	matches := make(model.PostSearchMatches, len(searchResult.Hits.Hits))

	for i, hit := range searchResult.Hits.Hits {
		postIds[i] = hit.Id_
		// Extract highlighted content if available
		if hit.Highlight != nil {
			highlights := []string{}
			for field, hlContent := range hit.Highlight {
				highlights = append(highlights, hlContent...)
			}
			matches[hit.Id_] = highlights
		}
	}

	return postIds, matches, nil
}

func (es *ElasticsearchEngine) DeletePost(post *model.Post) *model.AppError {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	if atomic.LoadInt32(&es.ready) == 0 {
		return model.NewAppError("ElasticsearchEngine.DeletePost", "searchengine.elasticsearch.not_started.error", nil, "", http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*es.Platform.Config().ElasticsearchSettings.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	_, err := es.client.API.Delete(strings.ToLower(*es.Platform.Config().ElasticsearchSettings.IndexPrefix + IndexBasePosts), post.Id).Do(ctx)
	if err != nil {
		return model.NewAppError("ElasticsearchEngine.DeletePost", "searchengine.elasticsearch.delete_post.error", nil, "", http.StatusInternalServerError).Wrap(err)
	}

	return nil
}

func (es *ElasticsearchEngine) DeleteChannelPosts(rctx request.CTX, channelID string) *model.AppError {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	if atomic.LoadInt32(&es.ready) == 0 {
		return model.NewAppError("ElasticsearchEngine.DeleteChannelPosts", "searchengine.elasticsearch.not_started.error", nil, "", http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*es.Platform.Config().ElasticsearchSettings.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	query := &types.Query{
		Match: map[string]types.MatchQuery{
			"channel_id": {
				Query: channelID,
			},
		},
	}

	_, err := es.client.API.DeleteByQuery([]string{strings.ToLower(*es.Platform.Config().ElasticsearchSettings.IndexPrefix + IndexBasePosts)}).
		Request(&deletebyquery.Request{
			Query: query,
		}).Do(ctx)

	if err != nil {
		return model.NewAppError("ElasticsearchEngine.DeleteChannelPosts", "searchengine.elasticsearch.delete_channel_posts.error", nil, "", http.StatusInternalServerError).Wrap(err)
	}

	return nil
}

func (es *ElasticsearchEngine) DeleteUserPosts(rctx request.CTX, userID string) *model.AppError {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	if atomic.LoadInt32(&es.ready) == 0 {
		return model.NewAppError("ElasticsearchEngine.DeleteUserPosts", "searchengine.elasticsearch.not_started.error", nil, "", http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*es.Platform.Config().ElasticsearchSettings.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	query := &types.Query{
		Match: map[string]types.MatchQuery{
			"user_id": {
				Query: userID,
			},
		},
	}

	_, err := es.client.API.DeleteByQuery([]string{strings.ToLower(*es.Platform.Config().ElasticsearchSettings.IndexPrefix + IndexBasePosts)}).
		Request(&deletebyquery.Request{
			Query: query,
		}).Do(ctx)

	if err != nil {
		return model.NewAppError("ElasticsearchEngine.DeleteUserPosts", "searchengine.elasticsearch.delete_user_posts.error", nil, "", http.StatusInternalServerError).Wrap(err)
	}

	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) IndexChannel(rctx request.CTX, channel *model.Channel, userIDs, teamMemberIDs []string) *model.AppError {
	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) SearchChannels(teamId, userID, term string, isGuest, includeDeleted bool) ([]string, *model.AppError) {
	return []string{}, nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) DeleteChannel(channel *model.Channel) *model.AppError {
	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) IndexUser(rctx request.CTX, user *model.User, teamsIds, channelsIds []string) *model.AppError {
	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) SearchUsersInChannel(teamId, channelId string, restrictedToChannels []string, term string, options *model.UserSearchOptions) ([]string, []string, *model.AppError) {
	return []string{}, []string{}, nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) SearchUsersInTeam(teamId string, restrictedToChannels []string, term string, options *model.UserSearchOptions) ([]string, *model.AppError) {
	return []string{}, nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) DeleteUser(user *model.User) *model.AppError {
	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) IndexFile(file *model.FileInfo, channelId string) *model.AppError {
	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) SearchFiles(channels model.ChannelList, searchParams []*model.SearchParams, page, perPage int) ([]string, *model.AppError) {
	return []string{}, nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) DeleteFile(fileID string) *model.AppError {
	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) DeletePostFiles(rctx request.CTX, postID string) *model.AppError {
	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) DeleteUserFiles(rctx request.CTX, userID string) *model.AppError {
	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) DeleteFilesBatch(rctx request.CTX, endTime, limit int64) *model.AppError {
	return nil
}

// Test the connection to Elasticsearch
func (es *ElasticsearchEngine) TestConfig(rctx request.CTX, cfg *model.Config) *model.AppError {
	// Validate the config
	if !*cfg.ElasticsearchSettings.EnableIndexing {
		return model.NewAppError("TestElasticsearch", "searchengine.elasticsearch.disabled.error", nil, "EnableIndexing is set to false", http.StatusBadRequest)
	}

	if *cfg.ElasticsearchSettings.ConnectionUrl == "" {
		return model.NewAppError("TestElasticsearch", "searchengine.elasticsearch.bad_connection.error", nil, "ConnectionURL is empty", http.StatusBadRequest)
	}

	// Try to connect with the new settings
	tempConfig := elastic.Config{
		Addresses: []string{*cfg.ElasticsearchSettings.ConnectionUrl},
		Username:  *cfg.ElasticsearchSettings.Username,
		Password:  *cfg.ElasticsearchSettings.Password,
	}

	client, err := elastic.NewTypedClient(tempConfig)
	if err != nil {
		return model.NewAppError("TestElasticsearch", "searchengine.elasticsearch.connection_error", nil, err.Error(), http.StatusBadRequest)
	}

	// Check if we can ping the server
	_, err = client.API.Info().Do(context.Background())
	if err != nil {
		return model.NewAppError("TestElasticsearch", "searchengine.elasticsearch.ping_failed.error", nil, err.Error(), http.StatusBadRequest)
	}

	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) PurgeIndexes(rctx request.CTX) *model.AppError {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	if atomic.LoadInt32(&es.ready) == 0 {
		return model.NewAppError("ElasticsearchEngine.PurgeIndexes", "searchengine.elasticsearch.not_started.error", nil, "", http.StatusInternalServerError)
	}

	// Get all indexes with the configured prefix
	ctx := context.Background()
	indexPrefix := *es.Platform.Config().ElasticsearchSettings.IndexPrefix
	indexes, err := es.client.API.Indices.Get("_all").Do(ctx)
	if err != nil {
		return model.NewAppError("ElasticsearchEngine.PurgeIndexes", "searchengine.elasticsearch.indices.get.error", nil, err.Error(), http.StatusInternalServerError)
	}

	// Find all indexes matching our prefix
	var indexesToDelete []string
	for name := range indexes {
		if strings.HasPrefix(name, indexPrefix) {
			indexesToDelete = append(indexesToDelete, name)
		}
	}

	// Delete all matching indexes
	if len(indexesToDelete) > 0 {
		_, err := es.client.API.Indices.Delete(indexesToDelete).Do(ctx)
		if err != nil {
			return model.NewAppError("ElasticsearchEngine.PurgeIndexes", "searchengine.elasticsearch.indices.delete.error", nil, err.Error(), http.StatusInternalServerError)
		}
	}

	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) PurgeIndexList(rctx request.CTX, indexes []string) *model.AppError {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	if atomic.LoadInt32(&es.ready) == 0 {
		return model.NewAppError("ElasticsearchEngine.PurgeIndexList", "searchengine.elasticsearch.not_started.error", nil, "", http.StatusInternalServerError)
	}

	// Delete specified indexes
	if len(indexes) > 0 {
		ctx := context.Background()
		_, err := es.client.API.Indices.Delete(indexes).Do(ctx)
		if err != nil {
			return model.NewAppError("ElasticsearchEngine.PurgeIndexList", "searchengine.elasticsearch.indices.delete.error", nil, err.Error(), http.StatusInternalServerError)
		}
	}

	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) RefreshIndexes(rctx request.CTX) *model.AppError {
	return nil
}

// Not implemented for the basic version
func (es *ElasticsearchEngine) DataRetentionDeleteIndexes(rctx request.CTX, cutoff time.Time) *model.AppError {
	return nil
} 