/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package wiz

import "regexp"

// The event types this plugin pulls. The names double as the logical
// identifiers used for tag routing, per-source config keys, and storage
// namespacing.
const (
	sourceVulnerability = "VulnerabilityFinding"
	sourceIssue         = "Issues"
	sourceDetection     = "Detection"
	sourceConfiguration = "ConfigurationFinding"
	sourceAudit         = "Audit"
)

// knownSources is the set of valid source names for config validation.
var knownSources = map[string]bool{
	sourceVulnerability: true,
	sourceIssue:         true,
	sourceDetection:     true,
	sourceConfiguration: true,
	sourceAudit:         true,
}

// Built-in GraphQL queries for each event type. Field names, filter shapes, and
// cursor fields were taken from the Wiz schema. Each query pages a Relay-style
// connection with $first/$after and filters server-side on its cursor field via
// $since, which the plugin feeds the tracked high-water-mark timestamp. Replace
// any of these per-source with Query-Override if you need different fields.
const (
	vulnerabilityQuery = `query WizVulnerabilityFindings($first: Int, $after: String, $since: DateTime) {
  vulnerabilityFindings(first: $first, after: $after, filterBy: {updatedAt: {after: $since}}) {
    nodes {
      id
      name
      detailedName
      vulnerabilityExternalId
      CVEDescription
      severity
      vendorSeverity
      nvdSeverity
      weightedSeverity
      CVSSSeverity
      score
      hasExploit
      hasCisaKevExploit
      hasFix
      status
      firstDetectedAt
      lastDetectedAt
      updatedAt
      resolvedAt
      statusUpdatedAt
      description
      remediation
      version
      fixedVersion
      detectionMethod
      validatedInRuntime
      epssProbability
      epssSeverity
      vulnerableAsset {
        ... on VulnerableAssetBase {
          id
          externalId
          providerUniqueId
          name
          type
          nativeType
          region
          cloudPlatform
          cloudProviderURL
          subscriptionExternalId
          subscriptionId
          subscriptionName
          status
          tags
          hasWideInternetExposure
          hasLimitedInternetExposure
        }
      }
    }
    pageInfo { hasNextPage endCursor }
  }
}`

	issueQuery = `query WizIssues($first: Int, $after: String, $since: DateTime) {
  issues(first: $first, after: $after, filterBy: {createdAt: {after: $since}}) {
    nodes {
      id
      type
      status
      severity
      createdAt
      updatedAt
      resolvedAt
      dueAt
      statusChangedAt
      firstEventAt
      lastEventAt
      resolutionReason
      description
      url
      entitySnapshot {
        id
        type
        nativeType
        name
        externalId
        providerId
        fullResourceName
        region
        cloudPlatform
        cloudProviderURL
        status
        subscriptionId
        subscriptionExternalId
        subscriptionName
        resourceGroupExternalId
        kubernetesClusterName
        kubernetesNamespaceName
        tags
      }
      sourceRule {
        id
        name
        description
      }
      control {
        id
        name
        type
        severity
      }
    }
    pageInfo { hasNextPage endCursor }
  }
}`

	detectionQuery = `query WizDetections($first: Int, $after: String, $since: DateTime) {
  detections(first: $first, after: $after, filterBy: {updatedAt: {after: $since}}) {
    nodes {
      id
      type
      severity
      startedAt
      endedAt
      createdAt
      updatedAt
      origins
      ignored
      isRetroactive
      ruleMatch {
        version
        rule {
          id
          name
          type
          severity
          description
        }
      }
      primaryActor {
        id
        externalId
        providerUniqueId
        type
        nativeType
        name
        friendlyName
        email
        IP
        userAgent
        hasAdminPrivileges
        hasHighPrivileges
      }
      primaryResource {
        id
        externalId
        providerUniqueId
        type
        nativeType
        name
        hostname
        region
        openToAllInternet
        hasSensitiveData
      }
    }
    pageInfo { hasNextPage endCursor }
  }
}`

	configurationQuery = `query WizConfigurationFindings($first: Int, $after: String, $since: DateTime) {
  configurationFindings(first: $first, after: $after, filterBy: {updatedAt: {after: $since}}) {
    nodes {
      id
      name
      type
      source
      result
      severity
      status
      resolutionReason
      analyzedAt
      firstSeenAt
      updatedAt
      statusChangedAt
      remediation
      targetExternalId
      targetObjectProviderUniqueId
      deleted
      rule {
        id
        name
        shortId
        severity
        description
        cloudProvider
        serviceType
        targetNativeType
      }
      resource {
        id
        name
        type
        nativeType
        providerId
        region
        cloudPlatform
        status
        isAccessibleFromInternet
        hasSensitiveData
      }
      subscription {
        id
        externalId
        name
        cloudProvider
      }
    }
    pageInfo { hasNextPage endCursor }
  }
}`

	auditQuery = `query WizAuditLogEntries($first: Int, $after: String, $since: DateTime) {
  auditLogEntries(first: $first, after: $after, filterBy: {timestamp: {after: $since}}) {
    nodes {
      id
      actionType
      action
      actionParameters
      requestId
      timestamp
      sourceIP
      sourceIPCountryCode
      userAgent
      clientType
      status
      duration
      user {
        id
        name
        email
      }
      serviceAccount {
        id
        name
        email
        description
        type
      }
    }
    pageInfo { hasNextPage endCursor }
  }
}`
)

// source describes one event type: its logical name, the GraphQL root field its
// connection is returned under, the query document, and the node field used as
// the incremental cursor (matching the server-side $since filter field).
type source struct {
	name    string
	field   string
	query   string
	tsField string

	// which pagination/filter variables the query document declares.
	hasFirst bool
	hasAfter bool
	hasSince bool
}

// builtinSources returns the default source set. A source's query can be
// overridden via config; overrides are applied by New.
func builtinSources() []source {
	return []source{
		{name: sourceVulnerability, field: "vulnerabilityFindings", query: vulnerabilityQuery, tsField: "updatedAt"},
		{name: sourceIssue, field: "issues", query: issueQuery, tsField: "createdAt"},
		{name: sourceDetection, field: "detections", query: detectionQuery, tsField: "updatedAt"},
		{name: sourceConfiguration, field: "configurationFindings", query: configurationQuery, tsField: "updatedAt"},
		{name: sourceAudit, field: "auditLogEntries", query: auditQuery, tsField: "timestamp"},
	}
}

// varDefRx matches GraphQL operation variable definitions, e.g. the "$first" in
// "query Foo($first: Int, $after: String)". A definition is always a "$name"
// immediately followed by a colon; variable uses (e.g. "after: $after") are
// not, so this matches only declarations.
var varDefRx = regexp.MustCompile(`\$(\w+)\s*:`)

// parseQueryVars returns the set of variable names declared by a GraphQL
// operation document.
func parseQueryVars(doc string) map[string]bool {
	vars := make(map[string]bool)
	for _, m := range varDefRx.FindAllStringSubmatch(doc, -1) {
		vars[m[1]] = true
	}
	return vars
}
