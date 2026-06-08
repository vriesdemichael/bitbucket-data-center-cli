package bulk

import (
	"fmt"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

const jsonSchemaVersion = "https://json-schema.org/draft/2020-12/schema"

func Schemas() map[string]map[string]any {
	return map[string]map[string]any{
		"bulk-policy.schema.json":       PolicyJSONSchema(),
		"bulk-plan.schema.json":         PlanJSONSchema(),
		"bulk-apply-status.schema.json": ApplyStatusJSONSchema(),
	}
}

func PolicyJSONSchema() map[string]any {
	return map[string]any{
		"$schema":              jsonSchemaVersion,
		"$id":                  schemaID("bulk-policy.schema.json"),
		"title":                "BBSC Bulk Policy",
		"description":          "Schema for multi-repository bulk policy files. JSON Schema can validate equivalent YAML documents.",
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"apiVersion": map[string]any{"const": APIVersion},
			"selector":   refSchema("Selector"),
			"operations": map[string]any{"type": "array", "minItems": 1, "items": refSchema("Operation")},
		},
		"required": s("apiVersion", "selector", "operations"),
		"$defs":    commonDefinitions(),
	}
}

func PlanJSONSchema() map[string]any {
	return map[string]any{
		"$schema":              jsonSchemaVersion,
		"$id":                  schemaID("bulk-plan.schema.json"),
		"title":                "BBSC Bulk Plan",
		"description":          "Schema for deterministic reviewed bulk plan artifacts produced by bb bulk plan.",
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"apiVersion": map[string]any{"const": APIVersion},
			"kind":       map[string]any{"const": PlanKind},
			"planHash":   planHashSchema(),
			"policy":     refSchema("Policy"),
			"validation": refSchema("ValidationResult"),
			"summary":    refSchema("PlanSummary"),
			"targets":    map[string]any{"type": "array", "minItems": 1, "items": refSchema("TargetPlan")},
		},
		"required": s("apiVersion", "kind", "planHash", "policy", "validation", "summary", "targets"),
		"$defs":    commonDefinitions(),
	}
}

func ApplyStatusJSONSchema() map[string]any {
	return map[string]any{
		"$schema":              jsonSchemaVersion,
		"$id":                  schemaID("bulk-apply-status.schema.json"),
		"title":                "BBSC Bulk Apply Status",
		"description":          "Schema for persisted and command-emitted bulk apply status artifacts produced by bb bulk apply and bb bulk status.",
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"apiVersion":  map[string]any{"const": APIVersion},
			"kind":        map[string]any{"const": ApplyStatusKind},
			"operationId": identifierSchema(),
			"planHash":    planHashSchema(),
			"status":      map[string]any{"type": "string", "enum": s(resultStatusSuccess, resultStatusFailed, "partial_failure")},
			"summary":     refSchema("ApplySummary"),
			"targets":     map[string]any{"type": "array", "minItems": 1, "items": refSchema("TargetResult")},
		},
		"required": s("apiVersion", "kind", "operationId", "planHash", "status", "summary", "targets"),
		"$defs":    commonDefinitions(),
	}
}

func commonDefinitions() map[string]any {
	return map[string]any{
		"Policy":                            policyDefinition(),
		"Selector":                          selectorDefinition(),
		"Operation":                         operationDefinition(),
		"RepoPermissionUserGrantOperation":  repoPermissionUserGrantOperationDefinition(),
		"RepoPermissionGroupGrantOperation": repoPermissionGroupGrantOperationDefinition(),
		"RepoWebhookCreateOperation":        repoWebhookCreateOperationDefinition(),
		"RepoPullRequestAllTasksOperation":  repoPullRequestAllTasksOperationDefinition(),
		"RepoPullRequestApproversOperation": repoPullRequestApproversOperationDefinition(),
		"BuildRequiredCreateOperation":      buildRequiredCreateOperationDefinition(),
		"RepoSettingsAutoMergeOperation":    repoSettingsAutoMergeOperationDefinition(),
		"RepoSettingsAutoDeclineOperation":  repoSettingsAutoDeclineOperationDefinition(),
		"RepoDefaultTaskCreateOperation":    repoDefaultTaskCreateOperationDefinition(),
		"RepositoryTarget":                  repositoryTargetDefinition(),
		"ValidationResult":                  validationResultDefinition(),
		"PlanSummary":                       planSummaryDefinition(),
		"TargetPlan":                        targetPlanDefinition(),
		"ApplySummary":                      applySummaryDefinition(),
		"OperationResult":                   operationResultDefinition(),
		"TargetResult":                      targetResultDefinition(),
		"RequiredBuildPayload":              requiredBuildPayloadDefinition(),
		"RefMatcher":                        refMatcherDefinition(),
	}
}

func policyDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"apiVersion": map[string]any{"const": APIVersion},
			"selector":   refSchema("Selector"),
			"operations": map[string]any{"type": "array", "minItems": 1, "items": refSchema("Operation")},
		},
		"required": s("apiVersion", "selector", "operations"),
	}
}

func selectorDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"projectKey":  nonEmptyStringSchema(),
			"repoPattern": nonEmptyStringSchema(),
			"repositories": map[string]any{
				"type":        "array",
				"minItems":    1,
				"uniqueItems": true,
				"items": map[string]any{
					"type":      "string",
					"minLength": 1,
					"pattern":   `^[^/\s]+(?:/[^/\s]+)?$`,
				},
			},
		},
		"allOf": []any{
			map[string]any{"anyOf": []any{
				map[string]any{"required": s("projectKey")},
				map[string]any{"required": s("repoPattern")},
				map[string]any{"required": s("repositories")},
			}},
			map[string]any{
				"if":   map[string]any{"required": s("repoPattern")},
				"then": map[string]any{"required": s("projectKey")},
			},
		},
	}
}

func operationDefinition() map[string]any {
	refs := make([]any, 0, len(supportedOperationTypes))
	for _, name := range []string{
		"RepoPermissionUserGrantOperation",
		"RepoPermissionGroupGrantOperation",
		"RepoWebhookCreateOperation",
		"RepoPullRequestAllTasksOperation",
		"RepoPullRequestApproversOperation",
		"BuildRequiredCreateOperation",
		"RepoSettingsAutoMergeOperation",
		"RepoSettingsAutoDeclineOperation",
		"RepoDefaultTaskCreateOperation",
	} {
		refs = append(refs, refSchema(name))
	}
	return map[string]any{"oneOf": refs}
}

func repoPermissionUserGrantOperationDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":       map[string]any{"const": OperationRepoPermissionUserGrant},
			"username":   nonEmptyStringSchema(),
			"permission": repositoryPermissionSchema(),
		},
		"required": s("type", "username", "permission"),
	}
}

func repoPermissionGroupGrantOperationDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":       map[string]any{"const": OperationRepoPermissionGroupGrant},
			"group":      nonEmptyStringSchema(),
			"permission": repositoryPermissionSchema(),
		},
		"required": s("type", "group", "permission"),
	}
}

func repoWebhookCreateOperationDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":   map[string]any{"const": OperationRepoWebhookCreate},
			"name":   nonEmptyStringSchema(),
			"url":    nonEmptyStringSchema(),
			"events": map[string]any{"type": "array", "minItems": 1, "items": nonEmptyStringSchema()},
			"active": map[string]any{"type": "boolean", "default": true},
		},
		"required": s("type", "name", "url"),
	}
}

func repoPullRequestAllTasksOperationDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":                     map[string]any{"const": OperationRepoPullRequestRequiredAllTasksComplete},
			"requiredAllTasksComplete": map[string]any{"type": "boolean"},
		},
		"required": s("type", "requiredAllTasksComplete"),
	}
}

func repoPullRequestApproversOperationDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":  map[string]any{"const": OperationRepoPullRequestRequiredApproversCount},
			"count": map[string]any{"type": "integer", "minimum": 0},
		},
		"required": s("type", "count"),
	}
}

func buildRequiredCreateOperationDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":    map[string]any{"const": OperationBuildRequiredCreate},
			"payload": refSchema("RequiredBuildPayload"),
		},
		"required": s("type", "payload"),
	}
}

func repositoryTargetDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"projectKey": nonEmptyStringSchema(),
			"slug":       nonEmptyStringSchema(),
		},
		"required": s("projectKey", "slug"),
	}
}

func validationResultDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"valid":  map[string]any{"type": "boolean"},
			"errors": map[string]any{"type": "array", "items": nonEmptyStringSchema()},
		},
		"required": s("valid", "errors"),
	}
}

func planSummaryDefinition() map[string]any {
	return countSummaryDefinition([]string{"targetCount", "operationCount"})
}

func targetPlanDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"repository": refSchema("RepositoryTarget"),
			"validation": refSchema("ValidationResult"),
			"operations": map[string]any{"type": "array", "minItems": 1, "items": refSchema("Operation")},
		},
		"required": s("repository", "validation", "operations"),
	}
}

func applySummaryDefinition() map[string]any {
	return countSummaryDefinition([]string{
		"targetCount",
		"operationCount",
		"successfulTargets",
		"failedTargets",
		"successfulOperations",
		"failedOperations",
		"skippedOperations",
	})
}

func operationResultDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":      map[string]any{"type": "string", "enum": s(supportedOperationTypes...)},
			"status":    map[string]any{"type": "string", "enum": s(resultStatusSuccess, resultStatusFailed, resultStatusSkipped)},
			"output":    map[string]any{},
			"error":     map[string]any{"type": "string"},
			"errorKind": map[string]any{"type": "string", "enum": s(errorKindEnum()...)},
		},
		"required": s("type", "status"),
	}
}

func targetResultDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"repository": refSchema("RepositoryTarget"),
			"status":     map[string]any{"type": "string", "enum": s(resultStatusSuccess, resultStatusFailed)},
			"operations": map[string]any{"type": "array", "minItems": 1, "items": refSchema("OperationResult")},
		},
		"required": s("repository", "status", "operations"),
	}
}

func requiredBuildPayloadDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"properties": map[string]any{
			"buildParentKeys":  map[string]any{"type": "array", "minItems": 1, "items": nonEmptyStringSchema()},
			"refMatcher":       refSchema("RefMatcher"),
			"exemptRefMatcher": refSchema("RefMatcher"),
		},
		"required": s("buildParentKeys", "refMatcher"),
	}
}

func refMatcherDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"properties": map[string]any{
			"id": nonEmptyStringSchema(),
		},
		"required": s("id"),
	}
}

func countSummaryDefinition(required []string) map[string]any {
	properties := map[string]any{}
	for _, name := range required {
		properties[name] = map[string]any{"type": "integer", "minimum": 0}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             s(required...),
	}
}

func repositoryPermissionSchema() map[string]any {
	return map[string]any{"type": "string", "enum": s("REPO_READ", "REPO_WRITE", "REPO_ADMIN")}
}

func nonEmptyStringSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1}
}

func refSchema(name string) map[string]any {
	return map[string]any{"$ref": fmt.Sprintf("#/$defs/%s", name)}
}

func schemaID(fileName string) string {
	return "https://github.com/vriesdemichael/bitbucket-server-cli/docs/reference/schemas/" + fileName
}

func planHashSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": `^sha256:[0-9a-f]{64}$`}
}

func identifierSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": `^[A-Za-z0-9._-]+$`}
}

func errorKindEnum() []string {
	return []string{
		string(apperrors.KindAuthentication),
		string(apperrors.KindAuthorization),
		string(apperrors.KindValidation),
		string(apperrors.KindNotFound),
		string(apperrors.KindConflict),
		string(apperrors.KindTransient),
		string(apperrors.KindPermanent),
		string(apperrors.KindNotImplemented),
		string(apperrors.KindInternal),
	}
}

func s(items ...string) []any {
	result := make([]any, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}

func repoSettingsAutoMergeOperationDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":    map[string]any{"const": OperationRepoSettingsAutoMerge},
			"enabled": map[string]any{"type": "boolean"},
		},
		"required": s("type", "enabled"),
	}
}

func repoSettingsAutoDeclineOperationDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":            map[string]any{"const": OperationRepoSettingsAutoDecline},
			"enabled":         map[string]any{"type": "boolean"},
			"inactivityWeeks": map[string]any{"type": "integer", "minimum": 1},
		},
		"required": s("type", "enabled"),
		"allOf": []any{
			map[string]any{
				"if": map[string]any{
					"properties": map[string]any{
						"enabled": map[string]any{"const": true},
					},
				},
				"then": map[string]any{
					"required": s("inactivityWeeks"),
				},
			},
		},
	}
}

func repoDefaultTaskCreateOperationDefinition() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":        map[string]any{"const": OperationRepoDefaultTaskCreate},
			"description": nonEmptyStringSchema(),
			"sourceRef":   nonEmptyStringSchema(),
			"targetRef":   nonEmptyStringSchema(),
		},
		"required": s("type", "description"),
	}
}
