package bulk

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/santhosh-tekuri/jsonschema/v6"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
	"gopkg.in/yaml.v3"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	APIVersion      = "bb.io/v1alpha1"
	PlanKind        = "BulkPlan"
	ApplyStatusKind = "BulkApplyStatus"

	OperationRepoPermissionUserGrant                 = "repo.permission.user.grant"
	OperationRepoPermissionGroupGrant                = "repo.permission.group.grant"
	OperationRepoWebhookCreate                       = "repo.webhook.create"
	OperationRepoPullRequestRequiredAllTasksComplete = "repo.pull-request-settings.required-all-tasks-complete"
	OperationRepoPullRequestRequiredApproversCount   = "repo.pull-request-settings.required-approvers-count"
	OperationBuildRequiredCreate                     = "build.required.create"
	OperationRepoSettingsAutoMerge                  = "repo.settings.auto-merge"
	OperationRepoSettingsAutoDecline                = "repo.settings.auto-decline"
	OperationRepoDefaultTaskCreate                  = "repo.default-task.create"

	resultStatusSuccess = "success"
	resultStatusFailed  = "failed"
	resultStatusSkipped = "skipped"
)

var supportedOperationTypes = []string{
	OperationRepoPermissionUserGrant,
	OperationRepoPermissionGroupGrant,
	OperationRepoWebhookCreate,
	OperationRepoPullRequestRequiredAllTasksComplete,
	OperationRepoPullRequestRequiredApproversCount,
	OperationBuildRequiredCreate,
	OperationRepoSettingsAutoMerge,
	OperationRepoSettingsAutoDecline,
	OperationRepoDefaultTaskCreate,
}

type Policy struct {
	APIVersion string          `yaml:"apiVersion" json:"apiVersion"`
	Selector   Selector        `yaml:"selector" json:"selector"`
	Operations []OperationSpec `yaml:"operations" json:"operations"`
}

type Selector struct {
	ProjectKey   string   `yaml:"projectKey,omitempty" json:"projectKey,omitempty"`
	RepoPattern  string   `yaml:"repoPattern,omitempty" json:"repoPattern,omitempty"`
	Repositories []string `yaml:"repositories,omitempty" json:"repositories,omitempty"`
}

type OperationSpec struct {
	Type                     string         `yaml:"type" json:"type"`
	Username                 string         `yaml:"username,omitempty" json:"username,omitempty"`
	Group                    string         `yaml:"group,omitempty" json:"group,omitempty"`
	Permission               string         `yaml:"permission,omitempty" json:"permission,omitempty"`
	Name                     string         `yaml:"name,omitempty" json:"name,omitempty"`
	URL                      string         `yaml:"url,omitempty" json:"url,omitempty"`
	Events                   []string       `yaml:"events,omitempty" json:"events,omitempty"`
	Active                   *bool          `yaml:"active,omitempty" json:"active,omitempty"`
	RequiredAllTasksComplete *bool          `yaml:"requiredAllTasksComplete,omitempty" json:"requiredAllTasksComplete,omitempty"`
	Count                    *int           `yaml:"count,omitempty" json:"count,omitempty"`
	Payload                  map[string]any `yaml:"payload,omitempty" json:"payload,omitempty"`
	Enabled                  *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	InactivityWeeks          *int           `yaml:"inactivityWeeks,omitempty" json:"inactivityWeeks,omitempty"`
	Description              *string        `yaml:"description,omitempty" json:"description,omitempty"`
	SourceRef                *string        `yaml:"sourceRef,omitempty" json:"sourceRef,omitempty"`
	TargetRef                *string        `yaml:"targetRef,omitempty" json:"targetRef,omitempty"`
}

type RepositoryTarget struct {
	ProjectKey string `json:"projectKey"`
	Slug       string `json:"slug"`
}

type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

type TargetPlan struct {
	Repository RepositoryTarget `json:"repository"`
	Validation ValidationResult `json:"validation"`
	Operations []OperationSpec  `json:"operations"`
}

type PlanSummary struct {
	TargetCount    int `json:"targetCount"`
	OperationCount int `json:"operationCount"`
}

type Plan struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	PlanHash   string           `json:"planHash"`
	Policy     Policy           `json:"policy"`
	Validation ValidationResult `json:"validation"`
	Summary    PlanSummary      `json:"summary"`
	Targets    []TargetPlan     `json:"targets"`
}

type OperationResult struct {
	Type      string `json:"type"`
	Status    string `json:"status"`
	Output    any    `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorKind string `json:"errorKind,omitempty"`
}

type TargetResult struct {
	Repository RepositoryTarget  `json:"repository"`
	Status     string            `json:"status"`
	Operations []OperationResult `json:"operations"`
}

type ApplySummary struct {
	TargetCount          int `json:"targetCount"`
	OperationCount       int `json:"operationCount"`
	SuccessfulTargets    int `json:"successfulTargets"`
	FailedTargets        int `json:"failedTargets"`
	SuccessfulOperations int `json:"successfulOperations"`
	FailedOperations     int `json:"failedOperations"`
	SkippedOperations    int `json:"skippedOperations"`
}

type ApplyStatus struct {
	APIVersion  string         `json:"apiVersion"`
	Kind        string         `json:"kind"`
	OperationID string         `json:"operationId"`
	PlanHash    string         `json:"planHash"`
	Status      string         `json:"status"`
	Summary     ApplySummary   `json:"summary"`
	Targets     []TargetResult `json:"targets"`
}

type RepositoryCatalog interface {
	ListByProject(ctx context.Context, projectKey string, opts repository.ListOptions) ([]repository.Repository, error)
}

type OperationRunner interface {
	Run(ctx context.Context, repo RepositoryTarget, operation OperationSpec) (any, error)
}

type Planner struct {
	catalog RepositoryCatalog
}

type Executor struct {
	runner OperationRunner
	store  *StatusStore
}

type StatusStore struct {
	baseDir string
}

func NewPlanner(catalog RepositoryCatalog) *Planner {
	return &Planner{catalog: catalog}
}

func NewExecutor(runner OperationRunner, store *StatusStore) *Executor {
	return &Executor{runner: runner, store: store}
}

func NewStatusStore(baseDir string) *StatusStore {
	return &StatusStore{baseDir: strings.TrimSpace(baseDir)}
}

func ParsePolicyYAML(raw []byte) (Policy, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return Policy{}, apperrors.New(apperrors.KindValidation, "bulk policy file is empty", nil)
	}

	var rawMap any
	if err := yaml.Unmarshal(raw, &rawMap); err != nil {
		return Policy{}, apperrors.New(apperrors.KindValidation, "failed to parse bulk policy YAML", err)
	}

	if err := validateSchema(PolicyJSONSchema(), rawMap, "bulk policy"); err != nil {
		return Policy{}, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)

	var policy Policy
	if err := decoder.Decode(&policy); err != nil {
		return Policy{}, apperrors.New(apperrors.KindValidation, "failed to decode bulk policy", err)
	}

	normalized, validationErrors := normalizePolicy(policy)
	if len(validationErrors) > 0 {
		return Policy{}, apperrors.New(apperrors.KindValidation, strings.Join(validationErrors, "; "), nil)
	}

	return normalized, nil
}

func LoadPlanJSON(raw []byte) (Plan, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return Plan{}, apperrors.New(apperrors.KindValidation, "bulk plan file is empty", nil)
	}

	var rawMap any
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return Plan{}, apperrors.New(apperrors.KindValidation, "failed to parse bulk plan JSON", err)
	}

	if err := validateSchema(PlanJSONSchema(), rawMap, "bulk plan"); err != nil {
		return Plan{}, err
	}

	var plan Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return Plan{}, apperrors.New(apperrors.KindValidation, "failed to decode bulk plan", err)
	}

	if err := VerifyPlan(plan); err != nil {
		return Plan{}, err
	}

	return plan, nil
}

func validateSchema(schemaMap map[string]any, data any, label string) error {
	compiler := jsonschema.NewCompiler()
	schemaURL := "schema.json"
	if err := compiler.AddResource(schemaURL, schemaMap); err != nil {
		return apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to compile %s schema", label), err)
	}

	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		return apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to compile %s schema", label), err)
	}

	if err := schema.Validate(data); err != nil {
		return apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s does not match schema: %v", label, err), err)
	}

	return nil
}

func VerifyPlan(plan Plan) error {
	if strings.TrimSpace(plan.APIVersion) != APIVersion {
		return apperrors.New(apperrors.KindValidation, fmt.Sprintf("bulk plan apiVersion must be %s", APIVersion), nil)
	}
	if strings.TrimSpace(plan.Kind) != PlanKind {
		return apperrors.New(apperrors.KindValidation, fmt.Sprintf("bulk plan kind must be %s", PlanKind), nil)
	}
	if strings.TrimSpace(plan.PlanHash) == "" {
		return apperrors.New(apperrors.KindValidation, "bulk plan is missing planHash", nil)
	}
	if !plan.Validation.Valid {
		return apperrors.New(apperrors.KindValidation, "bulk plan contains validation errors", nil)
	}
	if len(plan.Targets) == 0 {
		return apperrors.New(apperrors.KindValidation, "bulk plan contains no targets", nil)
	}

	expectedHash, err := computePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.PlanHash {
		return apperrors.New(apperrors.KindValidation, "bulk plan hash does not match the plan contents", nil)
	}

	return nil
}

func (planner *Planner) Plan(ctx context.Context, policy Policy) (Plan, error) {
	if planner == nil || planner.catalog == nil {
		return Plan{}, apperrors.New(apperrors.KindInternal, "bulk planner repository catalog is not configured", nil)
	}

	normalized, validationErrors := normalizePolicy(policy)
	if len(validationErrors) > 0 {
		return Plan{}, apperrors.New(apperrors.KindValidation, strings.Join(validationErrors, "; "), nil)
	}

	targets, err := planner.resolveTargets(ctx, normalized.Selector)
	if err != nil {
		return Plan{}, err
	}
	if len(targets) == 0 {
		return Plan{}, apperrors.New(apperrors.KindValidation, "selector resolved no repositories", nil)
	}

	targetPlans := make([]TargetPlan, 0, len(targets))
	for _, target := range targets {
		targetPlans = append(targetPlans, TargetPlan{
			Repository: target,
			Validation: ValidationResult{Valid: true, Errors: []string{}},
			Operations: copyOperations(normalized.Operations),
		})
	}

	plan := Plan{
		APIVersion: APIVersion,
		Kind:       PlanKind,
		Policy:     normalized,
		Validation: ValidationResult{Valid: true, Errors: []string{}},
		Summary: PlanSummary{
			TargetCount:    len(targetPlans),
			OperationCount: len(targetPlans) * len(normalized.Operations),
		},
		Targets: targetPlans,
	}

	planHash, err := computePlanHash(plan)
	if err != nil {
		return Plan{}, err
	}
	plan.PlanHash = planHash

	return plan, nil
}

func (executor *Executor) Apply(ctx context.Context, plan Plan) (ApplyStatus, error) {
	if executor == nil || executor.runner == nil {
		return ApplyStatus{}, apperrors.New(apperrors.KindInternal, "bulk executor runner is not configured", nil)
	}
	if err := VerifyPlan(plan); err != nil {
		return ApplyStatus{}, err
	}

	status := ApplyStatus{
		APIVersion:  APIVersion,
		Kind:        ApplyStatusKind,
		OperationID: newOperationID(),
		PlanHash:    plan.PlanHash,
		Status:      resultStatusSuccess,
		Summary: ApplySummary{
			TargetCount:    len(plan.Targets),
			OperationCount: plan.Summary.OperationCount,
		},
		Targets: make([]TargetResult, 0, len(plan.Targets)),
	}

	for _, target := range plan.Targets {
		targetResult := TargetResult{
			Repository: target.Repository,
			Status:     resultStatusSuccess,
			Operations: make([]OperationResult, 0, len(target.Operations)),
		}

		targetFailed := false
		for _, operation := range target.Operations {
			if targetFailed {
				targetResult.Operations = append(targetResult.Operations, OperationResult{
					Type:   operation.Type,
					Status: resultStatusSkipped,
				})
				status.Summary.SkippedOperations++
				continue
			}

			output, err := executor.runner.Run(ctx, target.Repository, operation)
			operationResult := OperationResult{
				Type:   operation.Type,
				Status: resultStatusSuccess,
				Output: output,
			}
			if err != nil {
				operationResult.Status = resultStatusFailed
				operationResult.Output = nil
				operationResult.Error = err.Error()
				if kind := strings.TrimSpace(string(apperrors.KindOf(err))); kind != "" {
					operationResult.ErrorKind = kind
				}
				targetResult.Status = resultStatusFailed
				targetFailed = true
				status.Summary.FailedOperations++
			} else {
				status.Summary.SuccessfulOperations++
			}

			targetResult.Operations = append(targetResult.Operations, operationResult)
		}

		if targetResult.Status == resultStatusSuccess {
			status.Summary.SuccessfulTargets++
		} else {
			status.Summary.FailedTargets++
		}

		status.Targets = append(status.Targets, targetResult)
	}

	if status.Summary.FailedTargets > 0 {
		if status.Summary.SuccessfulTargets == 0 {
			status.Status = resultStatusFailed
		} else {
			status.Status = "partial_failure"
		}
	}

	if executor.store != nil {
		if err := executor.store.Save(status); err != nil {
			return status, err
		}
	}

	return status, nil
}

func (status ApplyStatus) HasFailures() bool {
	return status.Summary.FailedTargets > 0 || status.Summary.FailedOperations > 0
}

func (store *StatusStore) Save(status ApplyStatus) error {
	if store == nil || strings.TrimSpace(store.baseDir) == "" {
		return apperrors.New(apperrors.KindInternal, "bulk status store directory is not configured", nil)
	}
	if err := validateIdentifier(status.OperationID, "operation id"); err != nil {
		return err
	}
	if err := os.MkdirAll(store.baseDir, 0o700); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to create bulk status directory", err)
	}

	encoded, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to encode bulk status", err)
	}

	if err := os.WriteFile(filepath.Join(store.baseDir, status.OperationID+".json"), encoded, 0o600); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to persist bulk status", err)
	}

	return nil
}

func (store *StatusStore) Load(operationID string) (ApplyStatus, error) {
	if store == nil || strings.TrimSpace(store.baseDir) == "" {
		return ApplyStatus{}, apperrors.New(apperrors.KindInternal, "bulk status store directory is not configured", nil)
	}
	if err := validateIdentifier(operationID, "operation id"); err != nil {
		return ApplyStatus{}, err
	}

	raw, err := os.ReadFile(filepath.Join(store.baseDir, strings.TrimSpace(operationID)+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return ApplyStatus{}, apperrors.New(apperrors.KindNotFound, fmt.Sprintf("bulk operation status %s was not found", strings.TrimSpace(operationID)), err)
		}
		return ApplyStatus{}, apperrors.New(apperrors.KindInternal, "failed to read bulk status", err)
	}

	var status ApplyStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return ApplyStatus{}, apperrors.New(apperrors.KindPermanent, "failed to decode bulk status", err)
	}
	if status.OperationID != strings.TrimSpace(operationID) {
		return ApplyStatus{}, apperrors.New(apperrors.KindPermanent, "bulk status payload does not match the requested operation id", nil)
	}
	if status.APIVersion != APIVersion || status.Kind != ApplyStatusKind {
		return ApplyStatus{}, apperrors.New(apperrors.KindPermanent, "bulk status payload uses an unsupported schema", nil)
	}

	return status, nil
}

func DescribeOperation(operation OperationSpec) string {
	switch operation.Type {
	case OperationRepoPermissionUserGrant:
		return fmt.Sprintf("grant user %s %s", operation.Username, operation.Permission)
	case OperationRepoPermissionGroupGrant:
		return fmt.Sprintf("grant group %s %s", operation.Group, operation.Permission)
	case OperationRepoWebhookCreate:
		return fmt.Sprintf("create webhook %s", operation.Name)
	case OperationRepoPullRequestRequiredAllTasksComplete:
		if operation.RequiredAllTasksComplete == nil {
			return "set requiredAllTasksComplete"
		}
		return fmt.Sprintf("set requiredAllTasksComplete=%t", *operation.RequiredAllTasksComplete)
	case OperationRepoPullRequestRequiredApproversCount:
		if operation.Count == nil {
			return "set requiredApprovers.count"
		}
		return fmt.Sprintf("set requiredApprovers.count=%d", *operation.Count)
	case OperationBuildRequiredCreate:
		return "create required build check"
	case OperationRepoSettingsAutoMerge:
		if operation.Enabled == nil {
			return "set auto-merge settings"
		}
		return fmt.Sprintf("set auto-merge enabled=%t", *operation.Enabled)
	case OperationRepoSettingsAutoDecline:
		if operation.Enabled == nil {
			return "set auto-decline settings"
		}
		if *operation.Enabled && operation.InactivityWeeks != nil {
			return fmt.Sprintf("set auto-decline enabled=%t inactivityWeeks=%d", *operation.Enabled, *operation.InactivityWeeks)
		}
		return fmt.Sprintf("set auto-decline enabled=%t", *operation.Enabled)
	case OperationRepoDefaultTaskCreate:
		desc := ""
		if operation.Description != nil {
			desc = *operation.Description
		}
		return fmt.Sprintf("create default task: %s", desc)
	default:
		return operation.Type
	}
}

func normalizePolicy(policy Policy) (Policy, []string) {
	normalized := policy
	validationErrors := make([]string, 0)

	normalized.APIVersion = strings.TrimSpace(policy.APIVersion)
	if normalized.APIVersion != APIVersion {
		validationErrors = append(validationErrors, fmt.Sprintf("apiVersion must be %s", APIVersion))
	}

	normalized.Selector.ProjectKey = strings.TrimSpace(policy.Selector.ProjectKey)
	normalized.Selector.RepoPattern = strings.TrimSpace(policy.Selector.RepoPattern)
	normalized.Selector.Repositories = make([]string, 0, len(policy.Selector.Repositories))
	for _, entry := range policy.Selector.Repositories {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			validationErrors = append(validationErrors, "selector.repositories entries must not be empty")
			continue
		}
		normalized.Selector.Repositories = append(normalized.Selector.Repositories, trimmed)
	}

	if normalized.Selector.ProjectKey == "" && normalized.Selector.RepoPattern != "" {
		validationErrors = append(validationErrors, "selector.projectKey is required when selector.repoPattern is set")
	}
	if normalized.Selector.ProjectKey == "" && normalized.Selector.RepoPattern == "" && len(normalized.Selector.Repositories) == 0 {
		validationErrors = append(validationErrors, "selector must include projectKey, repoPattern, or repositories")
	}

	if len(policy.Operations) == 0 {
		validationErrors = append(validationErrors, "operations must contain at least one entry")
	}

	normalized.Operations = make([]OperationSpec, 0, len(policy.Operations))
	for index, operation := range policy.Operations {
		normalizedOperation, operationErrors := normalizeOperation(operation)
		if len(operationErrors) > 0 {
			for _, operationError := range operationErrors {
				validationErrors = append(validationErrors, fmt.Sprintf("operations[%d]: %s", index, operationError))
			}
			continue
		}
		normalized.Operations = append(normalized.Operations, normalizedOperation)
	}

	return normalized, validationErrors
}

func normalizeOperation(operation OperationSpec) (OperationSpec, []string) {
	normalized := operation
	validationErrors := make([]string, 0)

	normalized.Type = strings.ToLower(strings.TrimSpace(operation.Type))
	if normalized.Type == "" {
		validationErrors = append(validationErrors, "type is required")
		return normalized, validationErrors
	}

	switch normalized.Type {
	case OperationRepoPermissionUserGrant:
		normalized.Username = strings.TrimSpace(operation.Username)
		normalized.Permission = strings.ToUpper(strings.TrimSpace(operation.Permission))
		if normalized.Username == "" {
			validationErrors = append(validationErrors, "username is required")
		}
		if !isRepositoryPermission(normalized.Permission) {
			validationErrors = append(validationErrors, "permission must be one of REPO_READ, REPO_WRITE, REPO_ADMIN")
		}
	case OperationRepoPermissionGroupGrant:
		normalized.Group = strings.TrimSpace(operation.Group)
		normalized.Permission = strings.ToUpper(strings.TrimSpace(operation.Permission))
		if normalized.Group == "" {
			validationErrors = append(validationErrors, "group is required")
		}
		if !isRepositoryPermission(normalized.Permission) {
			validationErrors = append(validationErrors, "permission must be one of REPO_READ, REPO_WRITE, REPO_ADMIN")
		}
	case OperationRepoWebhookCreate:
		normalized.Name = strings.TrimSpace(operation.Name)
		normalized.URL = strings.TrimSpace(operation.URL)
		if normalized.Name == "" {
			validationErrors = append(validationErrors, "name is required")
		}
		if normalized.URL == "" {
			validationErrors = append(validationErrors, "url is required")
		}
		normalized.Events = make([]string, 0, len(operation.Events))
		for _, event := range operation.Events {
			trimmedEvent := strings.TrimSpace(event)
			if trimmedEvent != "" {
				normalized.Events = append(normalized.Events, trimmedEvent)
			}
		}
		if len(normalized.Events) == 0 {
			normalized.Events = []string{"repo:refs_changed"}
		}
		if operation.Active == nil {
			normalized.Active = boolPtr(true)
		} else {
			normalized.Active = boolPtr(*operation.Active)
		}
	case OperationRepoPullRequestRequiredAllTasksComplete:
		if operation.RequiredAllTasksComplete == nil {
			validationErrors = append(validationErrors, "requiredAllTasksComplete is required")
		} else {
			normalized.RequiredAllTasksComplete = boolPtr(*operation.RequiredAllTasksComplete)
		}
	case OperationRepoPullRequestRequiredApproversCount:
		if operation.Count == nil {
			validationErrors = append(validationErrors, "count is required")
		} else if *operation.Count < 0 {
			validationErrors = append(validationErrors, "count must be >= 0")
		} else {
			value := *operation.Count
			normalized.Count = &value
		}
	case OperationBuildRequiredCreate:
		payload, err := normalizeMap(operation.Payload)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("payload is invalid: %v", err))
			break
		}
		if len(payload) == 0 {
			validationErrors = append(validationErrors, "payload is required")
			break
		}
		if _, err := json.Marshal(payload); err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("payload is invalid: %v", err))
			break
		}
		normalized.Payload = payload
	case OperationRepoSettingsAutoMerge:
		if operation.Enabled == nil {
			validationErrors = append(validationErrors, "enabled is required")
		} else {
			normalized.Enabled = boolPtr(*operation.Enabled)
		}
	case OperationRepoSettingsAutoDecline:
		if operation.Enabled == nil {
			validationErrors = append(validationErrors, "enabled is required")
		} else {
			normalized.Enabled = boolPtr(*operation.Enabled)
			if *operation.Enabled {
				if operation.InactivityWeeks == nil {
					validationErrors = append(validationErrors, "inactivityWeeks is required when enabled is true")
				} else if *operation.InactivityWeeks <= 0 {
					validationErrors = append(validationErrors, "inactivityWeeks must be > 0")
				} else {
					val := *operation.InactivityWeeks
					normalized.InactivityWeeks = &val
				}
			}
		}
	case OperationRepoDefaultTaskCreate:
		if operation.Description == nil || strings.TrimSpace(*operation.Description) == "" {
			validationErrors = append(validationErrors, "description is required")
		} else {
			desc := strings.TrimSpace(*operation.Description)
			normalized.Description = &desc
		}
		if operation.SourceRef != nil {
			ref := strings.TrimSpace(*operation.SourceRef)
			normalized.SourceRef = &ref
		}
		if operation.TargetRef != nil {
			ref := strings.TrimSpace(*operation.TargetRef)
			normalized.TargetRef = &ref
		}
	default:
		validationErrors = append(validationErrors, fmt.Sprintf("unsupported type %q (supported: %s)", normalized.Type, strings.Join(supportedOperationTypes, ", ")))
	}

	return normalized, validationErrors
}

func (planner *Planner) resolveTargets(ctx context.Context, selector Selector) ([]RepositoryTarget, error) {
	type projectIndex map[string]repository.Repository

	projectRepos := map[string]projectIndex{}
	loadProjectRepos := func(projectKey string) (projectIndex, error) {
		trimmedProjectKey := strings.TrimSpace(projectKey)
		if trimmedProjectKey == "" {
			return nil, apperrors.New(apperrors.KindValidation, "selector projectKey cannot be empty", nil)
		}
		if cached, ok := projectRepos[trimmedProjectKey]; ok {
			return cached, nil
		}

		repositories, err := planner.catalog.ListByProject(ctx, trimmedProjectKey, repository.ListOptions{Limit: 100})
		if err != nil {
			return nil, err
		}

		index := make(projectIndex, len(repositories))
		for _, repo := range repositories {
			index[repo.Slug] = repo
		}
		projectRepos[trimmedProjectKey] = index
		return index, nil
	}

	resolved := map[string]RepositoryTarget{}
	addTarget := func(projectKey, slug string) {
		resolved[projectKey+"/"+slug] = RepositoryTarget{ProjectKey: projectKey, Slug: slug}
	}

	if selector.ProjectKey != "" && selector.RepoPattern == "" && len(selector.Repositories) == 0 {
		projectIndex, err := loadProjectRepos(selector.ProjectKey)
		if err != nil {
			return nil, err
		}
		for _, repo := range sortedRepositories(projectIndex) {
			addTarget(repo.ProjectKey, repo.Slug)
		}
	}

	if selector.RepoPattern != "" {
		projectIndex, err := loadProjectRepos(selector.ProjectKey)
		if err != nil {
			return nil, err
		}
		for _, repo := range sortedRepositories(projectIndex) {
			matched, matchErr := path.Match(selector.RepoPattern, repo.Slug)
			if matchErr != nil {
				return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("selector.repoPattern is invalid: %v", matchErr), matchErr)
			}
			if matched {
				addTarget(repo.ProjectKey, repo.Slug)
			}
		}
	}

	for _, entry := range selector.Repositories {
		repoRef, err := resolveRepositoryEntry(selector.ProjectKey, entry)
		if err != nil {
			return nil, err
		}

		projectIndex, err := loadProjectRepos(repoRef.ProjectKey)
		if err != nil {
			return nil, err
		}
		repo, ok := projectIndex[repoRef.Slug]
		if !ok {
			return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("selector repository %s/%s was not found", repoRef.ProjectKey, repoRef.Slug), nil)
		}
		addTarget(repo.ProjectKey, repo.Slug)
	}

	targets := make([]RepositoryTarget, 0, len(resolved))
	for _, target := range resolved {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(left, right int) bool {
		if targets[left].ProjectKey == targets[right].ProjectKey {
			return targets[left].Slug < targets[right].Slug
		}
		return targets[left].ProjectKey < targets[right].ProjectKey
	})

	return targets, nil
}

func ComputePlanHash(plan Plan) (string, error) {
	clone := plan
	clone.PlanHash = ""

	encoded, err := json.Marshal(clone)
	if err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to encode bulk plan hash", err)
	}

	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func computePlanHash(plan Plan) (string, error) {
	return ComputePlanHash(plan)
}

func resolveRepositoryEntry(defaultProjectKey, entry string) (RepositoryTarget, error) {
	trimmed := strings.TrimSpace(entry)
	if trimmed == "" {
		return RepositoryTarget{}, apperrors.New(apperrors.KindValidation, "selector.repositories entries must not be empty", nil)
	}

	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 1 {
		if strings.TrimSpace(defaultProjectKey) == "" {
			return RepositoryTarget{}, apperrors.New(apperrors.KindValidation, fmt.Sprintf("selector repository %q must be in PROJECT/slug format when selector.projectKey is not set", trimmed), nil)
		}
		return RepositoryTarget{ProjectKey: strings.TrimSpace(defaultProjectKey), Slug: strings.TrimSpace(parts[0])}, nil
	}

	projectKey := strings.TrimSpace(parts[0])
	slug := strings.TrimSpace(parts[1])
	if projectKey == "" || slug == "" {
		return RepositoryTarget{}, apperrors.New(apperrors.KindValidation, fmt.Sprintf("selector repository %q must be in PROJECT/slug format", trimmed), nil)
	}

	return RepositoryTarget{ProjectKey: projectKey, Slug: slug}, nil
}

func sortedRepositories(index map[string]repository.Repository) []repository.Repository {
	repositories := make([]repository.Repository, 0, len(index))
	for _, repo := range index {
		repositories = append(repositories, repo)
	}
	sort.Slice(repositories, func(left, right int) bool {
		if repositories[left].ProjectKey == repositories[right].ProjectKey {
			return repositories[left].Slug < repositories[right].Slug
		}
		return repositories[left].ProjectKey < repositories[right].ProjectKey
	})
	return repositories
}

func copyOperations(operations []OperationSpec) []OperationSpec {
	if len(operations) == 0 {
		return []OperationSpec{}
	}

	cloned := make([]OperationSpec, 0, len(operations))
	for _, operation := range operations {
		copyOperation := operation
		if operation.Events != nil {
			copyOperation.Events = append([]string(nil), operation.Events...)
		}
		if operation.Payload != nil {
			copyOperation.Payload = cloneMap(operation.Payload)
		}
		if operation.Active != nil {
			copyOperation.Active = boolPtr(*operation.Active)
		}
		if operation.RequiredAllTasksComplete != nil {
			copyOperation.RequiredAllTasksComplete = boolPtr(*operation.RequiredAllTasksComplete)
		}
		if operation.Count != nil {
			value := *operation.Count
			copyOperation.Count = &value
		}
		if operation.Enabled != nil {
			copyOperation.Enabled = boolPtr(*operation.Enabled)
		}
		if operation.InactivityWeeks != nil {
			val := *operation.InactivityWeeks
			copyOperation.InactivityWeeks = &val
		}
		if operation.Description != nil {
			val := *operation.Description
			copyOperation.Description = &val
		}
		if operation.SourceRef != nil {
			val := *operation.SourceRef
			copyOperation.SourceRef = &val
		}
		if operation.TargetRef != nil {
			val := *operation.TargetRef
			copyOperation.TargetRef = &val
		}
		cloned = append(cloned, copyOperation)
	}

	return cloned
}

func cloneMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}

	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneValue(value)
	}
	return cloned
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, cloneValue(item))
		}
		return cloned
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func normalizeMap(values map[string]any) (map[string]any, error) {
	if values == nil {
		return nil, nil
	}

	normalized := make(map[string]any, len(values))
	for key, value := range values {
		normalizedValue, err := normalizeValue(value)
		if err != nil {
			return nil, err
		}
		normalized[key] = normalizedValue
	}

	return normalized, nil
}

func normalizeValue(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		return normalizeMap(typed)
	case map[any]any:
		normalized := make(map[string]any, len(typed))
		for key, nested := range typed {
			stringKey, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("payload keys must be strings")
			}
			normalizedValue, err := normalizeValue(nested)
			if err != nil {
				return nil, err
			}
			normalized[stringKey] = normalizedValue
		}
		return normalized, nil
	case []any:
		normalized := make([]any, 0, len(typed))
		for _, item := range typed {
			normalizedValue, err := normalizeValue(item)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, normalizedValue)
		}
		return normalized, nil
	default:
		if _, err := json.Marshal(typed); err != nil {
			return nil, err
		}
		return typed, nil
	}
}

func validateIdentifier(value string, label string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s is required", label), nil)
	}
	if strings.ContainsAny(trimmed, `/\\`) {
		return apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s cannot contain path separators", label), nil)
	}
	for _, character := range trimmed {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s contains unsupported characters", label), nil)
	}
	return nil
}

func newOperationID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err == nil {
		return "op-" + hex.EncodeToString(buffer)
	}
	return fmt.Sprintf("op-%d", time.Now().UnixNano())
}

func boolPtr(value bool) *bool {
	return &value
}

func isRepositoryPermission(permission string) bool {
	switch permission {
	case "REPO_READ", "REPO_WRITE", "REPO_ADMIN":
		return true
	default:
		return false
	}
}
