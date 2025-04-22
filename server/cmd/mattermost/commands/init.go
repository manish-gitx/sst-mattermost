// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/i18n"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/public/shared/request"
	"github.com/mattermost/mattermost/server/v8/channels/app"
	"github.com/mattermost/mattermost/server/v8/channels/store"
	"github.com/mattermost/mattermost/server/v8/channels/store/sqlstore"
	"github.com/mattermost/mattermost/server/v8/channels/utils"
	"github.com/mattermost/mattermost/server/v8/config"

	// Enterprise Deps
	_ "github.com/gorilla/handlers"
	_ "github.com/hako/durafmt"
	_ "github.com/splitio/go-client/v6/splitio"
	_ "github.com/tylerb/graceful"

	// Enterprise Imports
	_ "github.com/mattermost/mattermost/server/v8/enterprise/account_migration"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/admin_guide"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/announcement"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/audits"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/authenticator"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/commands"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/compliance"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/data_retention"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/dashboard"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/elasticsearch"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/imports"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/ingestion"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/licensing"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/message_export"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/metrics"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/mfa"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/openapi"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/playbooks"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/remote_cluster"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/saml"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/sandbox"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/search"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/service_control"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/shared_channels"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/support"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/system"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/team_limits"
	_ "github.com/mattermost/mattermost/server/v8/enterprise/user_retention"
	_ "github.com/mattermost/mattermost/server/v8/platform/services/searchengine/elasticsearch"
)

func initDBCommandContextCobra(command *cobra.Command, readOnlyConfigStore bool, options ...app.Option) (*app.App, error) {
	a, err := initDBCommandContext(getConfigDSN(command, config.GetEnvironment()), readOnlyConfigStore, options...)
	if err != nil {
		// Returning an error just prints the usage message, so actually panic
		panic(err)
	}

	a.InitPlugins(request.EmptyContext(a.Log()), *a.Config().PluginSettings.Directory, *a.Config().PluginSettings.ClientDirectory)
	a.DoAppMigrations()

	return a, nil
}

func InitDBCommandContextCobra(command *cobra.Command, options ...app.Option) (*app.App, error) {
	return initDBCommandContextCobra(command, true, options...)
}

func initDBCommandContext(configDSN string, readOnlyConfigStore bool, options ...app.Option) (*app.App, error) {
	if err := utils.TranslationsPreInit(); err != nil {
		return nil, err
	}
	model.AppErrorInit(i18n.T)

	// The option order is important as app.Config option reads app.StartMetrics option.
	options = append(options, app.Config(configDSN, readOnlyConfigStore, nil))
	s, err := app.NewServer(options...)
	if err != nil {
		return nil, err
	}

	a := app.New(app.ServerConnector(s.Channels()))

	if model.BuildEnterpriseReady == "true" {
		a.Srv().LoadLicense()
	}

	return a, nil
}

func initStoreCommandContextCobra(logger mlog.LoggerIFace, command *cobra.Command) (store.Store, error) {
	cfgDSN := getConfigDSN(command, config.GetEnvironment())
	cfgStore, err := config.NewStoreFromDSN(cfgDSN, true, nil, true)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load configuration")
	}

	config := cfgStore.Get()
	return sqlstore.New(config.SqlSettings, logger, nil)
}
