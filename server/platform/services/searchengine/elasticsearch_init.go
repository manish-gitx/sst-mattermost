// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package searchengine

import (
	// Import the elasticsearch package to ensure its init() function is called
	_ "github.com/mattermost/mattermost/server/v8/platform/services/searchengine/elasticsearch"
) 