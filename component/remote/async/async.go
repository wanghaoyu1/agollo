/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package async

import (
	"encoding/json"
	"fmt"
	"github.com/zouyx/agollo/v4/component/log"
	"github.com/zouyx/agollo/v4/component/remote"
	"github.com/zouyx/agollo/v4/component/remote/abs"
	"github.com/zouyx/agollo/v4/constant"
	"github.com/zouyx/agollo/v4/env"
	"github.com/zouyx/agollo/v4/env/config"
	"github.com/zouyx/agollo/v4/extension"
	"github.com/zouyx/agollo/v4/protocol/http"
	"github.com/zouyx/agollo/v4/utils"
	"net/url"
	"path"
	"time"
)

const (
	//notify timeout
	notifyConnectTimeout = 10 * time.Minute //10m

	defaultContentKey = "content"
)

var (
	remoteApollo apolloConfig
)

func init() {
	remoteApollo = apolloConfig{}
}

func GetInstance() remote.ApolloConfig {
	return &remoteApollo
}

type apolloConfig struct {
	abs.ApolloConfig
}

func (*apolloConfig) GetNotifyURLSuffix(notifications string, config config.AppConfig) string {
	return fmt.Sprintf("notifications/v2?appId=%s&cluster=%s&notifications=%s",
		url.QueryEscape(config.AppID),
		url.QueryEscape(config.Cluster),
		url.QueryEscape(notifications))
}

func (*apolloConfig) GetSyncURI(config config.AppConfig, namespaceName string) string {
	return fmt.Sprintf("configs/%s/%s/%s?releaseKey=%s&ip=%s",
		url.QueryEscape(config.AppID),
		url.QueryEscape(config.Cluster),
		url.QueryEscape(namespaceName),
		url.QueryEscape(config.GetCurrentApolloConfig().GetReleaseKey(namespaceName)),
		utils.GetInternal())
}

func (a *apolloConfig) Sync(appConfig *config.AppConfig) []*config.ApolloConfig {
	remoteConfigs, err := a.notifyRemoteConfig(appConfig, utils.Empty)

	var apolloConfigs []*config.ApolloConfig
	if err != nil || len(remoteConfigs) == 0 {
		apolloConfigs = loadBackupConfig(appConfig.NamespaceName, appConfig)
	}

	if len(apolloConfigs) > 0 {
		return apolloConfigs
	}

	appConfig.GetNotificationsMap().UpdateAllNotifications(remoteConfigs)

	notifications := appConfig.GetNotificationsMap().GetNotifications()
	n := &notifications
	n.Range(func(key, value interface{}) bool {
		apolloConfig := a.SyncWithNamespace(key.(string), appConfig)
		apolloConfigs = append(apolloConfigs, apolloConfig)
		return true
	})
	return apolloConfigs
}

func (*apolloConfig) CallBack() http.CallBack {
	return http.CallBack{
		SuccessCallBack:   createApolloConfigWithJSON,
		NotModifyCallBack: touchApolloConfigCache,
	}
}

func (a *apolloConfig) notifyRemoteConfig(appConfig *config.AppConfig, namespace string) ([]*config.Notification, error) {
	if appConfig == nil {
		panic("can not find apollo config!please confirm!")
	}
	urlSuffix := a.GetNotifyURLSuffix(appConfig.GetNotificationsMap().GetNotifies(namespace), *appConfig)

	connectConfig := &env.ConnectConfig{
		URI:    urlSuffix,
		AppID:  appConfig.AppID,
		Secret: appConfig.Secret,
	}
	connectConfig.Timeout = notifyConnectTimeout
	notifies, err := http.RequestRecovery(appConfig, connectConfig, &http.CallBack{
		SuccessCallBack: func(responseBody []byte) (interface{}, error) {
			return toApolloConfig(responseBody)
		},
		NotModifyCallBack: touchApolloConfigCache,
	})

	if notifies == nil {
		return nil, err
	}

	return notifies.([]*config.Notification), err
}

func touchApolloConfigCache() error {
	return nil
}

func toApolloConfig(resBody []byte) ([]*config.Notification, error) {
	remoteConfig := make([]*config.Notification, 0)

	err := json.Unmarshal(resBody, &remoteConfig)

	if err != nil {
		log.Error("Unmarshal Msg Fail,Error:", err)
		return nil, err
	}
	return remoteConfig, nil
}

func loadBackupConfig(namespace string, appConfig *config.AppConfig) []*config.ApolloConfig {
	apolloConfigs := make([]*config.ApolloConfig, 0)
	config.SplitNamespaces(namespace, func(namespace string) {
		c, err := extension.GetFileHandler().LoadConfigFile(appConfig.BackupConfigPath, namespace)
		if err != nil {
			log.Error("LoadConfigFile error, error", err)
			return
		}
		if c == nil {
			return
		}
		apolloConfigs = append(apolloConfigs, c)
	})
	return apolloConfigs
}

func createApolloConfigWithJSON(b []byte) (o interface{}, err error) {
	apolloConfig := &config.ApolloConfig{}
	err = json.Unmarshal(b, apolloConfig)
	if utils.IsNotNil(err) {
		return nil, err
	}

	parser := extension.GetFormatParser(constant.ConfigFileFormat(path.Ext(apolloConfig.NamespaceName)))
	if parser == nil {
		parser = extension.GetFormatParser(constant.DEFAULT)
	}

	if parser == nil {
		return apolloConfig, nil
	}
	m, err := parser.Parse(apolloConfig.Configurations[defaultContentKey])
	if err != nil {
		log.Debug("GetContent fail ! error:", err)
	}

	if len(m) > 0 {
		apolloConfig.Configurations = m
	}
	return apolloConfig, nil
}
