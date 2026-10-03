/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"fmt"
	"path"
)

const (
	// login field names
	USER_FIELD string = "User"
	PASS_FIELD string = "Pass"

	// API paths
	LOGIN_URL                             = `/api/login`
	LOGOUT_URL                            = `/api/logout`
	MFA_URL                               = `/api/mfa`
	MFA_CLEAR_URL                         = `/api/mfa/clear`
	LOGIN_MFA_URL                         = `/api/login/mfa`
	MFA_TOTP_SETUP_URL                    = `/api/mfa/totp/setup`
	MFA_TOTP_CLEAR_URL                    = `/api/mfa/totp/clear`
	MFA_RECOVERY_CODES_GENERATE_URL       = "/api/mfa/recovery-codes/generate"
	LOGIN_TEMPORARY_TOKEN_URL             = `/api/login/temporary-token`
	LOGIN_REFRESH_TOKEN_URL               = `/api/login/refresh-token`
	SELF_URL                              = `/api/self`
	STATS_SYSTEM_DESCRIPTION_URL          = `/api/stats/system-description`
	STATS_PING_URL                        = `/api/stats/ping`
	STATS_SYSTEM_URL                      = `/api/stats/system`
	STATS_INDEXERS_URL                    = `/api/stats/indexers`
	STATS_INGESTERS_URL                   = `/api/stats/ingesters`
	STATS_INGESTER_TAIL_URL               = `/api/stats/ingester-tail`
	STATS_WELLSTATS_URL                   = `/api/stats/wellStats`
	STATS_SEARCH_QUEUE_URL                = `/api/stats/search-queue`
	STATS_INDEXER_STORAGE_URL             = `/api/stats/indexer-storage`
	INDEXERS_ID_WELLS_URL                 = `/api/indexers/%s/wells`
	STATS_STORAGE_CALENDAR_URL            = `/api/stats/storage-calendar`
	INDEXERS_ID_STORAGE_CALENDAR_URL      = `/api/indexers/%s/storage-calendar`
	USERS_URL                             = `/api/users`
	LIST_USERS_URL                        = `/api/list/users`
	USERS_ID_URL                          = `/api/users/%d`
	USERS_ID_LOCK_URL                     = `/api/users/%d/lock`
	USERS_ID_LOCKED_URL                   = `/api/users/%d/locked`
	USERS_ID_PREFERENCES_URL              = `/api/users/%d/preferences`
	USERS_ID_ADMIN_URL                    = `/api/users/%d/admin`
	USERS_ID_SU_URL                       = `/api/users/%d/su`
	USERS_ID_SESSIONS_URL                 = `/api/users/%d/sessions`
	USERS_ID_PASSWORD_URL                 = `/api/users/%d/password`
	USERS_ID_GROUPS_URL                   = `/api/users/%d/groups`
	USERS_ID_GROUPS_GROUP_ID_URL          = `/api/users/%d/groups/%d`
	USERS_ID_MFA_CLEAR_URL                = `/api/users/%d/mfa/clear`
	WS_STATS_URL                          = `/api/ws/stats`
	WS_SEARCH_URL                         = `/api/ws/search`
	WS_ATTACH_ID_URL                      = `/api/ws/attach/%s`
	VALIDATE_QUERY_URL                    = `/api/validate/query`
	VERSION_URL                           = `/api/version`
	GROUPS_ID_URL                         = `/api/groups/%d`
	GROUPS_ID_MEMBERS_URL                 = `/api/groups/%d/members`
	GROUPS_URL                            = `/api/groups`
	LIST_GROUPS_URL                       = `/api/list/groups`
	LIST_SEARCHES_URL                     = `/api/list/searches`
	SEARCHES_ID_URL                       = `/api/searches/%s`
	SEARCHES_ID_ACCESS_URL                = `/api/searches/%s/access`
	SEARCHES_ID_ATTACH_URL                = `/api/searches/%s/attach`
	SEARCHES_ID_EXTRACTOR_SUGGESTIONS_URL = `/api/searches/%s/extractor-suggestions`
	SEARCHES_ID_BACKGROUND_URL            = `/api/searches/%s/background`
	SEARCHES_ID_SAVE_URL                  = `/api/searches/%s/save`
	SEARCHES_ID_STOP_URL                  = `/api/searches/%s/stop`
	SEARCHES_ID_DOWNLOADS_URL             = `/api/searches/%s/downloads`
	SEARCHES_ID_PING_URL                  = `/api/searches/%s/ping`
	SEARCHES_ID_DETACH_URL                = `/api/searches/%s/detach`
	SEARCHES_ID_MODULES_URL               = `/api/searches/%s/modules`
	SEARCHES_ID_STATS_URL                 = `/api/searches/%s/stats`
	SEARCHES_ID_STATS_METADATA_URL        = `/api/searches/%s/stats/metadata`
	SEARCHES_ID_EXPLORE_URL               = `/api/searches/%s/explore/%s`
	SEARCHES_ID_RENDERER_URL              = `/api/searches/%s/renderer/%s`
	IMPORT_PERSISTENT_SEARCH_URL          = `/api/import/persistent-search`
	SEARCHES_URL                          = `/api/searches`
	SEARCH_HISTORY_URL                    = `/api/search-history`
	LIST_SEARCH_HISTORY_URL               = `/api/list/search-history`
	SEARCH_HISTORY_ID_URL                 = `/api/search-history/%s`
	LIST_NOTIFICATIONS_URL                = `/api/list/notifications`
	NOTIFICATIONS_ID_URL                  = `/api/notifications/%d`
	NOTIFICATIONS_URL                     = `/api/notifications`
	LOGGING_URL                           = `/api/logging`
	TEST_URL                              = `/api/test`
	TEST_AUTHENTICATION_URL               = `/api/test/authentication`
	DASHBOARDS_ID_URL                     = `/api/dashboards/%v`
	DASHBOARDS_URL                        = `/api/dashboards`
	LIST_DASHBOARDS_URL                   = `/api/list/dashboards`
	MACROS_URL                            = `/api/macros`
	LIST_MACROS_URL                       = `/api/list/macros`
	MACROS_ID_URL                         = `/api/macros/%s`
	LICENSE_URL                           = `/api/license`
	LICENSE_SKU_URL                       = `/api/license/sku`
	LICENSE_SERIAL_URL                    = `/api/license/serial`
	LICENSE_UPDATE_URL                    = `/api/license/update`
	RESOURCES_URL                         = "/api/resources"
	LIST_RESOURCES_URL                    = "/api/list/resources"
	RESOURCES_ID_URL                      = "/api/resources/%s"
	RESOURCES_ID_CONTENT_URL              = "/api/resources/%s/content"
	RESOURCES_ID_CLONE_URL                = "/api/resources/%s/clone"
	LOOKUP_RESOURCES_NAME_URL             = "/api/lookup/resources/%s" // may be able to be removed using the new list/resource
	SCHEDULED_SEARCHES_URL                = "/api/scheduled-searches"
	LIST_SCHEDULED_SEARCHES_URL           = "/api/list/scheduled-searches"
	SCHEDULED_SEARCHES_ID_URL             = "/api/scheduled-searches/%s"
	SCHEDULED_SEARCHES_ID_RESULTS_URL     = "/api/scheduled-searches/%s/results"
	SCHEDULED_SEARCHES_ID_DEBUG_URL       = "/api/scheduled-searches/%s/debug"
	SCHEDULED_SEARCHES_ID_CANCEL_URL      = "/api/scheduled-searches/%s/cancel"
	SEARCH_AGENT_CHECKIN_URL              = "/api/search-agent/checkin"
	SCHEDULED_SCRIPTS_URL                 = "/api/scheduled-scripts"
	LIST_SCHEDULED_SCRIPTS_URL            = "/api/list/scheduled-scripts"
	SCHEDULED_SCRIPTS_ID_URL              = "/api/scheduled-scripts/%s"
	SCHEDULED_SCRIPTS_ID_RESULTS_URL      = "/api/scheduled-scripts/%s/results"
	SCHEDULED_SCRIPTS_ID_DEBUG_URL        = "/api/scheduled-scripts/%s/debug"
	SCHEDULED_SCRIPTS_ID_CANCEL_URL       = "/api/scheduled-scripts/%s/cancel"
	SCHEDULED_SCRIPTS_CHECKIN_URL         = "/api/scheduled-scripts/checkin"
	VALIDATE_SCHEDULED_SCRIPT_URL         = "/api/validate/scheduled-script"
	FLOWS_URL                             = "/api/flows"
	LIST_FLOWS_URL                        = "/api/list/flows"
	FLOWS_ID_URL                          = "/api/flows/%v"
	FLOWS_ID_RESULTS_URL                  = "/api/flows/%s/results"
	FLOWS_ID_DEBUG_URL                    = "/api/flows/%s/debug"
	FLOWS_ID_CANCEL_URL                   = "/api/flows/%s/cancel"
	VALIDATE_FLOW_URL                     = "/api/validate/flow"
	MAIL_URL                              = "/api/mail"
	SELF_MAIL_CONFIGURATION_URL           = `/api/self/mail-configuration`
	INGEST_JSON_URL                       = "/api/ingest/json"
	INGEST_LINES_URL                      = "/api/ingest/lines"
	INGEST_INTERNAL_URL                   = "/api/ingest/internal"
	INGEST_TEST_URL                       = "/api/ingest/test"
	LIST_TAGS_URL                         = "/api/list/tags"
	INDEXERS_URL                          = "/api/indexers"
	KITS_URL                              = `/api/kits`
	LIST_KITS_URL                         = `/api/list/kits`
	KITS_ID_URL                           = `/api/kits/%s`
	KIT_BUILDS_URL                        = `/api/kit-builds`
	KIT_BUILDS_ID_URL                     = `/api/kit-builds/%s`
	LIST_KIT_INSTALLS_URL                 = `/api/list/kit-installs`
	KIT_INSTALLS_ID_URL                   = `/api/kit-installs/%v`
	LIST_REMOTE_KITS_URL                  = `/api/list/remote-kits`
	KIT_BUILD_REQUESTS_URL                = `/api/kit-build-requests`
	LIST_KIT_BUILD_REQUESTS_URL           = `/api/list/kit-build-requests`
	KIT_BUILD_REQUESTS_ID_URL             = `/api/kit-build-requests/%s`
	AUTO_EXTRACTORS_URL                   = `/api/auto-extractors`
	LIST_AUTO_EXTRACTORS_URL              = `/api/list/auto-extractors`
	AUTO_EXTRACTORS_UPLOAD_URL            = `/api/auto-extractors/upload`
	VALIDATE_AUTO_EXTRACTOR_URL           = `/api/validate/auto-extractor`
	AUTO_EXTRACTORS_ID_URL                = `/api/auto-extractors/%s`
	TAGS_TAG_AUTO_EXTRACTOR_URL           = `/api/tags/%s/auto-extractor`
	INFO_EXTRACTOR_ENGINES_URL            = `/api/info/extractor-engines`
	TEMPLATES_URL                         = "/api/templates"
	LIST_TEMPLATES_URL                    = "/api/list/templates"
	TEMPLATES_ID_URL                      = "/api/templates/%s"
	TEMPLATES_ID_DETAILS_URL              = "/api/templates/%s/details"
	ACTIONABLES_URL                       = "/api/actionables"
	LIST_ACTIONABLES_URL                  = "/api/list/actionables"
	ACTIONABLES_ID_URL                    = "/api/actionables/%s"
	FILES_URL                             = "/api/files"
	LIST_FILES_URL                        = "/api/list/files"
	FILES_ID_URL                          = "/api/files/%s"
	FILES_ID_CONTENT_URL                  = "/api/files/%s/content"
	SAVED_QUERIES_URL                     = "/api/saved-queries"
	SAVED_QUERIES_ID_URL                  = "/api/saved-queries/%s"
	LIST_SAVED_QUERIES_URL                = "/api/list/saved-queries"
	LIBS_URL                              = `/api/libs`
	INFO_CAPABILITIES_URL                 = `/api/info/capabilities`
	INFO_CAPABILITY_TEMPLATES_URL         = `/api/info/capability-templates`
	SELF_CAPABILITIES_URL                 = `/api/self/capabilities`
	SELF_CAPABILITY_EXPLANATIONS_URL      = `/api/self/capability-explanations`
	USERS_ID_CAPABILITIES_URL             = `/api/users/%d/capabilities`
	USERS_ID_CAPABILITY_EXPLANATIONS_URL  = `/api/users/%d/capability-explanations`
	GROUPS_ID_CAPABILITIES_URL            = `/api/groups/%d/capabilities`
	GROUPS_ID_TAGS_URL                    = `/api/groups/%d/tags`
	USERS_ID_TAGS_URL                     = `/api/users/%d/tags`
	PLAYBOOKS_URL                         = `/api/playbooks`
	LIST_PLAYBOOKS_URL                    = `/api/list/playbooks`
	PLAYBOOKS_ID_URL                      = `/api/playbooks/%s`
	BACKUP_URL                            = `/api/backup`
	INFO_DEPLOYMENT_URL                   = `/api/info/deployment`
	TOKENS_URL                            = `/api/tokens`
	LIST_TOKENS_URL                       = `/api/list/tokens`
	TOKENS_ID_URL                         = `/api/tokens/%s`
	TOKENS_ID_REGENERATE_URL              = `/api/tokens/%s/regenerate`
	INFO_TOKEN_CAPABILITIES_URL           = `/api/info/token-capabilities`
	SECRETS_URL                           = `/api/secrets`
	LIST_SECRETS_URL                      = `/api/list/secrets`
	SECRETS_ID_URL                        = `/api/secrets/%s`
	SECRETS_ID_VALUE_URL                  = `/api/secrets/%s/value`
	SECRETS_ID_FULL_URL                   = `/api/secrets/%s/full`
	INFO_SETTINGS_URL                     = `/api/info/settings`
	INGESTERS_TRACKING_URL                = `/api/ingesters/tracking`
	ALERTS_URL                            = `/api/alerts`
	LIST_ALERTS_URL                       = `/api/list/alerts`
	ALERTS_ID_URL                         = `/api/alerts/%s`
	ALERTS_ID_SAMPLE_URL                  = `/api/alerts/%s/sample`
	VALIDATE_ALERT_DISPATCHER_URL         = `/api/validate/alert-dispatcher`
	VALIDATE_ALERT_CONSUMER_URL           = `/api/validate/alert-consumer`
	USER_PREFERENCES_URL                  = `/api/user-preferences`
	LIST_USER_PREFERENCES_URL             = `/api/list/user-preferences`
	USER_PREFERENCES_ID_URL               = `/api/user-preferences/%s`
	LIST_ASSETS_URL                       = `/api/list/assets`
	// Special APIs for installing licenses
	LICENSE_INIT_UPLOAD = `/license`
	LICENSE_INIT_STATUS = `/license/status`
)

func usersIdLockUrl(id int32) string {
	return fmt.Sprintf(USERS_ID_LOCK_URL, id)
}

func usersIdLockedUrl(id int32) string {
	return fmt.Sprintf(USERS_ID_LOCKED_URL, id)
}

func usersIdAdminUrl(id int32) string {
	return fmt.Sprintf(USERS_ID_ADMIN_URL, id)
}

func usersIdSuUrl(id int32) string {
	return fmt.Sprintf(USERS_ID_SU_URL, id)
}

func usersIdUrl(id int32) string {
	return fmt.Sprintf(USERS_ID_URL, id)
}

func usersIdPasswordUrl(id int32) string {
	return fmt.Sprintf(USERS_ID_PASSWORD_URL, id)
}

func usersIdGroupsUrl(uid int32) string {
	return fmt.Sprintf(USERS_ID_GROUPS_URL, uid)
}

func usersIdGroupsGroupIdUrl(uid, gid int32) string {
	return fmt.Sprintf(USERS_ID_GROUPS_GROUP_ID_URL, uid, gid)
}

func searchHistoryIdUrl(id string) string {
	return fmt.Sprintf(SEARCH_HISTORY_ID_URL, id)
}

func listSearchHistoryUrl() string {
	return LIST_SEARCH_HISTORY_URL
}

func groupsUrl() string {
	return GROUPS_URL
}

func groupsIdUrl(gid int32) string {
	return fmt.Sprintf(GROUPS_ID_URL, gid)
}

func groupsIdMembersUrl(gid int32) string {
	return fmt.Sprintf(GROUPS_ID_MEMBERS_URL, gid)
}

func dashboardsIdUrl(id string) string {
	return fmt.Sprintf(DASHBOARDS_ID_URL, id)
}

func usersUrl() string {
	return USERS_URL
}

func searchesIdAccessUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_ACCESS_URL, id)
}

func searchesIdBackgroundUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_BACKGROUND_URL, id)
}

func searchesIdSaveUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_SAVE_URL, id)
}

func searchesIdDownloadsUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_DOWNLOADS_URL, id)
}

func searchesIdStopUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_STOP_URL, id)
}

func importPersistentSearchUrl() string {
	return IMPORT_PERSISTENT_SEARCH_URL
}

func searchesIdUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_URL, id)
}

func usersIdSessionsUrl(id int32) string {
	return fmt.Sprintf(USERS_ID_SESSIONS_URL, id)
}

func usersIdPreferencesUrl(id int32) string {
	return fmt.Sprintf(USERS_ID_PREFERENCES_URL, id)
}

func notificationsIdUrl(id uint64) string {
	return fmt.Sprintf(NOTIFICATIONS_ID_URL, id)
}

func notificationsUrl() string {
	return NOTIFICATIONS_URL
}

func licenseUrl() string {
	return LICENSE_URL
}

func licenseSkuUrl() string {
	return LICENSE_SKU_URL
}

func licenseSerialUrl() string {
	return LICENSE_SERIAL_URL
}

func licenseUpdateUrl() string {
	return LICENSE_UPDATE_URL
}

func resourcesUrl() string {
	return RESOURCES_URL
}

func resourcesIdUrl(id string) string {
	return fmt.Sprintf(RESOURCES_ID_URL, id)
}

func resourcesIdContentUrl(id string) string {
	return fmt.Sprintf(RESOURCES_ID_CONTENT_URL, id)
}

func lookupResourcesNameUrl(name string) string {
	return fmt.Sprintf(LOOKUP_RESOURCES_NAME_URL, name)
}

func resourcesIdCloneUrl(id string) string {
	return fmt.Sprintf(RESOURCES_ID_CLONE_URL, id)
}

func scheduledSearchesUrl() string {
	return SCHEDULED_SEARCHES_URL
}

func scheduledSearchesIdUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SEARCHES_ID_URL, id)
}

func scheduledSearchesIdResultsUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SEARCHES_ID_RESULTS_URL, id)
}

func scheduledSearchesIdDebugUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SEARCHES_ID_DEBUG_URL, id)
}

func scheduledSearchesIdCancelUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SEARCHES_ID_CANCEL_URL, id)
}

func scheduledScriptsUrl() string {
	return SCHEDULED_SCRIPTS_URL
}

func validateScheduledScriptUrl() string {
	return VALIDATE_SCHEDULED_SCRIPT_URL
}

func scheduledScriptsIdUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SCRIPTS_ID_URL, id)
}

func scheduledScriptsIdResultsUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SCRIPTS_ID_RESULTS_URL, id)
}

func scheduledScriptsIdDebugUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SCRIPTS_ID_DEBUG_URL, id)
}

func scheduledScriptsIdCancelUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SCRIPTS_ID_CANCEL_URL, id)
}

func searchAgentCheckinUrl() string {
	return SEARCH_AGENT_CHECKIN_URL
}
func flowsUrl() string {
	return FLOWS_URL
}

func validateFlowUrl() string {
	return VALIDATE_FLOW_URL
}

func flowsIdUrl(id any) string {
	return fmt.Sprintf(FLOWS_ID_URL, id)
}

func flowsIdResultsUrl(id string) string {
	return fmt.Sprintf(FLOWS_ID_RESULTS_URL, id)
}

func flowsIdDebugUrl(id string) string {
	return fmt.Sprintf(FLOWS_ID_DEBUG_URL, id)
}

func flowsIdCancelUrl(id string) string {
	return fmt.Sprintf(FLOWS_ID_CANCEL_URL, id)
}

func loggingUrl() string {
	return LOGGING_URL
}

func loggingAccessUrl() string {
	return path.Join(LOGGING_URL, "access")
}

func loggingInfoUrl() string {
	return path.Join(LOGGING_URL, "info")
}

func loggingWarnUrl() string {
	return path.Join(LOGGING_URL, "warn")
}

func loggingErrorUrl() string {
	return path.Join(LOGGING_URL, "error")
}

func indexersUrl() string {
	return INDEXERS_URL
}

func statsWellstatsUrl() string {
	return STATS_WELLSTATS_URL
}

func statsSearchQueueUrl() string {
	return STATS_SEARCH_QUEUE_URL
}

func macrosIdUrl(id string) string {
	return fmt.Sprintf(MACROS_ID_URL, id)
}

func playbooksIdUrl(id string) string {
	return fmt.Sprintf(PLAYBOOKS_ID_URL, id)
}

func kitsUrl() string {
	return KITS_URL
}

func listRemoteKitsUrl(all bool) string {
	if all {
		return LIST_REMOTE_KITS_URL + "?all=true"
	}
	return LIST_REMOTE_KITS_URL
}

func kitsIdUrl(id string) string {
	return fmt.Sprintf(KITS_ID_URL, id)
}

func kitBuildsUrl() string {
	return KIT_BUILDS_URL
}

func kitBuildsIdUrl(id string) string {
	return fmt.Sprintf(KIT_BUILDS_ID_URL, id)
}

func listKitInstallsUrl() string {
	return LIST_KIT_INSTALLS_URL
}

func kitInstallsIdUrl(id int) string {
	return fmt.Sprintf(KIT_INSTALLS_ID_URL, id)
}

func kitBuildRequestsUrl() string {
	return KIT_BUILD_REQUESTS_URL
}

func kitBuildRequestsIdUrl(id string) string {
	return fmt.Sprintf(KIT_BUILD_REQUESTS_ID_URL, id)
}

func autoExtractorsUrl() string {
	return AUTO_EXTRACTORS_URL
}

func autoExtractorsUploadUrl() string {
	return AUTO_EXTRACTORS_UPLOAD_URL
}

func validateAutoExtractorUrl() string {
	return VALIDATE_AUTO_EXTRACTOR_URL
}

func autoExtractorsIdUrl(id string) string {
	return fmt.Sprintf(AUTO_EXTRACTORS_ID_URL, id)
}

func tagsTagAutoExtractorUrl(tag string) string {
	return fmt.Sprintf(TAGS_TAG_AUTO_EXTRACTOR_URL, tag)
}

func infoExtractorEnginesUrl() string {
	return INFO_EXTRACTOR_ENGINES_URL
}

func searchesIdExtractorSuggestionsUrl(searchId string) string {
	return fmt.Sprintf(SEARCHES_ID_EXTRACTOR_SUGGESTIONS_URL, searchId)
}

func templatesUrl() string {
	return TEMPLATES_URL
}

func templatesIdUrl(id string) string {
	return fmt.Sprintf(TEMPLATES_ID_URL, id)
}

func actionablesIdUrl(id string) string {
	return fmt.Sprintf(ACTIONABLES_ID_URL, id)
}

func filesUrl() string {
	return FILES_URL
}

func filesIdUrl(id string) string {
	return fmt.Sprintf(FILES_ID_URL, id)
}

func filesIdContentUrl(id string) string {
	return fmt.Sprintf(FILES_ID_CONTENT_URL, id)
}

func savedQueriesUrl() string {
	return SAVED_QUERIES_URL
}

func savedQueriesIdUrl(id string) string {
	return fmt.Sprintf(SAVED_QUERIES_ID_URL, id)
}

func backupUrl() string {
	return BACKUP_URL
}

func infoDeploymentUrl() string {
	return INFO_DEPLOYMENT_URL
}

func tokensUrl() string {
	return TOKENS_URL
}

func tokensIdUrl(id string) string {
	return fmt.Sprintf(TOKENS_ID_URL, id)
}

func tokensIdRegenerateUrl(id string) string {
	return fmt.Sprintf(TOKENS_ID_REGENERATE_URL, id)
}

func infoTokenCapabilitiesUrl() string {
	return INFO_TOKEN_CAPABILITIES_URL
}

func secretsUrl() string {
	return SECRETS_URL
}

func secretsIdUrl(id string) string {
	return fmt.Sprintf(SECRETS_ID_URL, id)
}

func secretsIdValueUrl(id string) string {
	return fmt.Sprintf(SECRETS_ID_VALUE_URL, id)
}

func secretsIdFullUrl(id string) string {
	return fmt.Sprintf(SECRETS_ID_FULL_URL, id)
}

func searchesUrl() string {
	return SEARCHES_URL
}

func searchesIdPingUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_PING_URL, id)
}

func searchesIdDetachUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_DETACH_URL, id)
}

func searchesIdStatsMetadataUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_STATS_METADATA_URL, id)
}

func searchesIdStatsUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_STATS_URL, id)
}

func searchesIdModulesUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_MODULES_URL, id)
}

func searchesIdExploreUrl(id, rndr string) string {
	return fmt.Sprintf(SEARCHES_ID_EXPLORE_URL, id, rndr)
}

func searchesIdRendererUrl(id, rndr string) string {
	return fmt.Sprintf(SEARCHES_ID_RENDERER_URL, id, rndr)
}

func validateQueryUrl() string {
	return VALIDATE_QUERY_URL
}

func searchesIdAttachUrl(id string) string {
	return fmt.Sprintf(SEARCHES_ID_ATTACH_URL, id)
}

func alertsUrl() string {
	return ALERTS_URL
}

func alertsIdUrl(id string) string {
	return fmt.Sprintf(ALERTS_ID_URL, id)
}

func alertsIdSampleUrl(id string) string {
	return fmt.Sprintf(ALERTS_ID_SAMPLE_URL, id)
}

func validateAlertDispatcherUrl() string {
	return VALIDATE_ALERT_DISPATCHER_URL
}

func validateAlertConsumerUrl() string {
	return VALIDATE_ALERT_CONSUMER_URL
}

func mfaTotpSetupUrl() string {
	return MFA_TOTP_SETUP_URL
}

func mfaTotpClearUrl() string {
	return MFA_TOTP_CLEAR_URL
}

func loginMfaUrl() string {
	return LOGIN_MFA_URL
}

func mfaUrl() string {
	return MFA_URL
}

func usersIdMfaClearUrl(uid int32) string {
	return fmt.Sprintf(USERS_ID_MFA_CLEAR_URL, uid)
}

func mfaClearUrl() string {
	return MFA_CLEAR_URL
}

func mfaRecoveryCodesGenerateUrl() string {
	return MFA_RECOVERY_CODES_GENERATE_URL
}

func userPreferencesIdUrl(id string) string {
	return fmt.Sprintf(USER_PREFERENCES_ID_URL, id)
}
