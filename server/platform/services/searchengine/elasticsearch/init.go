// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package elasticsearch

import (
	"github.com/mattermost/mattermost/server/v8/channels/app/platform"
	"github.com/mattermost/mattermost/server/v8/platform/services/searchengine"
)

func init() {
	platform.RegisterElasticsearchInterface(func(ps *platform.PlatformService) searchengine.SearchEngineInterface {
		return NewElasticsearchEngine(ps)
	})
} 