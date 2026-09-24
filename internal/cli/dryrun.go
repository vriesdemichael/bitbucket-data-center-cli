package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

type dryRunProfile struct {
	Intent   string
	Action   string
	Stateful bool
	// Tier is the strongest tier the command's preview reaches, on the path a
	// run takes when every check can be made. A preview may report less -- pr
	// merge on a Bitbucket that does not say whether a pull request can merge
	// is predicted -- and never more; the live suite holds every preview it
	// sees to that. A profile that checks nothing is predicted.
	Tier dryrunpreview.Tier
	// Change says in words what a command that changes this machine would
	// do, for the preview's reason: its Action is only create, update or
	// delete.
	Change string
}

// DeclaredDryRunTier is the strongest tier a command's preview reaches, as its
// profile declares it, for the live suite to hold previews to.
func DeclaredDryRunTier(path string) (dryrunpreview.Tier, bool) {
	profile, ok := dryRunProfiles[strings.TrimSpace(path)]
	if !ok {
		if _, local := clientLocalMutatingCommands[strings.TrimSpace(path)]; local {
			return dryrunpreview.TierPredicted, true
		}
		return "", false
	}
	if !profile.Stateful || profile.Tier == "" {
		return dryrunpreview.TierPredicted, true
	}

	return profile.Tier, true
}

type dryRunItem = dryrunpreview.Item
type dryRunPreview = dryrunpreview.Preview

// dryRunProfiles is the single source of truth for dry-run behaviour on every
// mutating command. Stateful: true means the command handler performs its own
// live pre-flight check and writes the dryRunPreview output itself; the
// interceptor passes through to it unchanged. Stateful: false means the
// interceptor generates a static (intent-only) preview using newDryRunPreview.
var dryRunProfiles = map[string]dryRunProfile{
	// api (raw REST passthrough escape hatch)
	"api": {Intent: "api.request", Action: "execute", Stateful: true, Tier: dryrunpreview.TierServerValidated},
	// branch
	"branch delete":             {Intent: "branch.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierServerValidated},
	"branch create":             {Intent: "branch.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"branch default set":        {Intent: "branch.default.set", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"branch model update":       {Intent: "branch.model.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"branch restriction create": {Intent: "branch.restriction.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"branch restriction update": {Intent: "branch.restriction.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"branch restriction delete": {Intent: "branch.restriction.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	// build
	"build status set":      {Intent: "build.status.set", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"build set":             {Intent: "build.set", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"build delete":          {Intent: "build.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"build required create": {Intent: "build.required.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"build required update": {Intent: "build.required.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"build required delete": {Intent: "build.required.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	// deployment
	"deployment create": {Intent: "deployment.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"deployment delete": {Intent: "deployment.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	// tag
	"tag create": {Intent: "tag.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"tag delete": {Intent: "tag.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	// repo comment
	"repo comment create": {Intent: "repo.comment.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo comment update": {Intent: "repo.comment.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo comment delete": {Intent: "repo.comment.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	// repo settings
	"repo settings workflow webhooks create":           {Intent: "repo.webhook.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo settings workflow webhooks delete":           {Intent: "repo.webhook.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo settings auto-merge set":                     {Intent: "repo.settings.auto-merge.set", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo settings auto-merge delete":                  {Intent: "repo.settings.auto-merge.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo settings auto-decline set":                   {Intent: "repo.settings.auto-decline.set", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo settings auto-decline delete":                {Intent: "repo.settings.auto-decline.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo settings pull-requests update":               {Intent: "repo.pull-request-settings.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo settings pull-requests update-approvers":     {Intent: "repo.pull-request-settings.update-approvers", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo settings pull-requests set-strategy":         {Intent: "repo.pull-request-settings.set-strategy", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo settings security permissions users grant":   {Intent: "repo.permission.user.grant", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo settings security permissions users revoke":  {Intent: "repo.permission.user.revoke", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo settings security permissions groups grant":  {Intent: "repo.permission.group.grant", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo settings security permissions groups revoke": {Intent: "repo.permission.group.revoke", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	// The shallow aliases of the four above. Their subject is a --group flag
	// rather than a path segment, so the intent they emit is decided at run
	// time; the Intent recorded here names the default (user) case. Stateful
	// entries build their own preview, so this field is documentation.
	"repo permissions grant":  {Intent: "repo.permission.user.grant", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo permissions revoke": {Intent: "repo.permission.user.revoke", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	// repo
	"repo create":              {Intent: "repo.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo fork":                {Intent: "repo.fork", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo delete":              {Intent: "repo.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo edit":                {Intent: "repo.edit", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo label add":           {Intent: "repo.label.add", Action: "create", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo label remove":        {Intent: "repo.label.remove", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo watch":               {Intent: "repo.watch", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo unwatch":             {Intent: "repo.unwatch", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo default-task add":    {Intent: "repo.default-task.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo default-task update": {Intent: "repo.default-task.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo default-task delete": {Intent: "repo.default-task.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	// webhook
	"webhook create": {Intent: "repo.webhook.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"webhook update": {Intent: "repo.webhook.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"webhook delete": {Intent: "repo.webhook.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"webhook test":   {Intent: "repo.webhook.test", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	// repo admin
	"repo admin create": {Intent: "repo.admin.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo admin fork":   {Intent: "repo.admin.fork", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo admin update": {Intent: "repo.admin.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"repo admin delete": {Intent: "repo.admin.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	// insights
	"insights report set":        {Intent: "insights.report.set", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"insights report delete":     {Intent: "insights.report.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"insights annotation add":    {Intent: "insights.annotation.add", Action: "create", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"insights annotation set":    {Intent: "insights.annotation.set", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"insights annotation delete": {Intent: "insights.annotation.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	// pr
	"pr create":                   {Intent: "pr.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"pr update":                   {Intent: "pr.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr merge":                    {Intent: "pr.merge", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr decline":                  {Intent: "pr.decline", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr reopen":                   {Intent: "pr.reopen", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr review approve":           {Intent: "pr.review.approve", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr review unapprove":         {Intent: "pr.review.unapprove", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr review set":               {Intent: "pr.review.set", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr review reviewer add":      {Intent: "pr.review.reviewer.add", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"pr review reviewer remove":   {Intent: "pr.review.reviewer.remove", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr review complete":          {Intent: "pr.review.complete", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr review discard":           {Intent: "pr.review.discard", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"pr comment add":              {Intent: "pr.comment.add", Action: "create", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"pr comment react":            {Intent: "pr.comment.react", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr comment resolve":          {Intent: "pr.comment.resolve", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"pr comment reopen":           {Intent: "pr.comment.reopen", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"pr comment apply-suggestion": {Intent: "pr.comment.apply-suggestion", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"pr auto-merge enable":        {Intent: "pr.auto-merge.enable", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr auto-merge disable":       {Intent: "pr.auto-merge.disable", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr watch":                    {Intent: "pr.watch", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"pr unwatch":                  {Intent: "pr.unwatch", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"pr rebase":                   {Intent: "pr.rebase", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"pr ready":                    {Intent: "pr.ready", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	// reviewer conditions
	"reviewer condition create": {Intent: "reviewer.condition.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"reviewer condition update": {Intent: "reviewer.condition.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"reviewer condition delete": {Intent: "reviewer.condition.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	// reviewer groups
	"reviewer-group create": {Intent: "reviewer-group.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"reviewer-group update": {Intent: "reviewer-group.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"reviewer-group delete": {Intent: "reviewer-group.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	// project
	"project create":                    {Intent: "project.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"project update":                    {Intent: "project.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"project delete":                    {Intent: "project.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project permissions users grant":   {Intent: "project.permission.user.grant", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project permissions users revoke":  {Intent: "project.permission.user.revoke", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project permissions groups grant":  {Intent: "project.permission.group.grant", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project permissions groups revoke": {Intent: "project.permission.group.revoke", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	// Shallow aliases of the four above; see the note on the repo pair.
	"project permissions grant":         {Intent: "project.permission.user.grant", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project permissions revoke":        {Intent: "project.permission.user.revoke", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project webhook create":            {Intent: "project.webhook.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project webhook update":            {Intent: "project.webhook.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project webhook delete":            {Intent: "project.webhook.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project webhook test":              {Intent: "project.webhook.test", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project branch-restriction create": {Intent: "project.branch-restriction.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"project branch-restriction update": {Intent: "project.branch-restriction.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPreconditionsChecked},
	"project branch-restriction delete": {Intent: "project.branch-restriction.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project default-task add":          {Intent: "project.default-task.create", Action: "create", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project default-task update":       {Intent: "project.default-task.update", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"project default-task delete":       {Intent: "project.default-task.delete", Action: "delete", Stateful: true, Tier: dryrunpreview.TierPredicted},
	// auth token
	"auth token create": {Intent: "auth.token.create", Action: "create", Stateful: false},
	"auth token update": {Intent: "auth.token.update", Action: "update", Stateful: false},
	"auth token revoke": {Intent: "auth.token.revoke", Action: "delete", Stateful: false},
	// auth gpg-key
	"auth gpg-key add":    {Intent: "auth.gpg-key.add", Action: "create", Stateful: false},
	"auth gpg-key remove": {Intent: "auth.gpg-key.remove", Action: "delete", Stateful: false},
	"auth gpg-key clear":  {Intent: "auth.gpg-key.clear", Action: "delete", Stateful: false},
	// ssh-key
	"ssh-key add":    {Intent: "ssh-key.add", Action: "create", Stateful: false},
	"ssh-key remove": {Intent: "ssh-key.remove", Action: "delete", Stateful: false},
	// repo ssh-key
	"repo ssh-key add":    {Intent: "repo.ssh-key.add", Action: "create", Stateful: false},
	"repo ssh-key remove": {Intent: "repo.ssh-key.remove", Action: "delete", Stateful: false},
	// repo sync
	"repo sync":         {Intent: "repo.sync.trigger", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo sync enable":  {Intent: "repo.sync.enable", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
	"repo sync disable": {Intent: "repo.sync.disable", Action: "update", Stateful: true, Tier: dryrunpreview.TierPredicted},
}

type commandClassification int

const (
	classificationUnknown commandClassification = iota
	classificationMutating
	classificationReadOnly

	// classificationLocal is a command that changes nothing, or that honours
	// --dry-run itself. The interceptor lets it run.
	classificationLocal

	// classificationLocalMutating is a command that changes state on this
	// machine -- stored credentials, git configuration, a file, a working copy
	// -- and does not honour --dry-run itself.
	//
	// It used to sit in classificationLocal, which means "let it run", so
	// `bb auth logout --dry-run` deleted the credentials and reported it in the
	// past tense. The flag's help said "server mutations" and these are local,
	// which is a defence on paper: a user who watches --dry-run delete their
	// credentials does not re-read the flag description, they stop trusting the
	// flag (#571).
	classificationLocalMutating

	// classificationWithoutDryRun is a command that does not take --dry-run at
	// all, because there is nothing a preview of it could say (ADR-096).
	classificationWithoutDryRun
)

var readOnlyCommands = map[string]struct{}{
	"admin health":                    {},
	"ai mcp tools":                    {},
	"ai skill show":                   {},
	"auth gpg-key list":               {},
	"auth identity":                   {},
	"auth status":                     {},
	"auth token get":                  {},
	"auth token list":                 {},
	"auth token-url":                  {},
	"branch default get":              {},
	"branch list":                     {},
	"branch model inspect":            {},
	"branch restriction get":          {},
	"branch restriction list":         {},
	"browse":                          {},
	"build get":                       {},
	"build required list":             {},
	"build status get":                {},
	"build status stats":              {},
	"commit compare":                  {},
	"commit get":                      {},
	"commit list":                     {},
	"commit prs":                      {},
	"deployment get":                  {},
	"diff commit":                     {},
	"diff pr":                         {},
	"diff refs":                       {},
	"doctor":                          {},
	"insights annotation list":        {},
	"insights report get":             {},
	"insights report list":            {},
	"pr activity list":                {},
	"pr auto-merge get":               {},
	"pr build status":                 {},
	"pr checks":                       {},
	"pr comment get":                  {},
	"pr comment list":                 {},
	"pr commits":                      {},
	"pr default-reviewers":            {},
	"pr diff":                         {},
	"pr files":                        {},
	"pr get":                          {},
	"pr jira":                         {},
	"pr list":                         {},
	"pr merge-base":                   {},
	"pr participants":                 {},
	"pr review get":                   {},
	"pr status":                       {},
	"project branch-restriction get":  {},
	"project branch-restriction list": {},
	"project default-task list":       {},
	"project get":                     {},
	"project list":                    {},
	"project permissions groups list": {},
	"project permissions list":        {},
	"project permissions show":        {},
	"project permissions users list":  {},
	"project webhook list":            {},
	"project webhook stats":           {},
	"ref list":                        {},
	"ref resolve":                     {},
	"repo archive":                    {},
	"repo browse blame":               {},
	"repo browse file":                {},
	"repo browse history":             {},
	"repo browse raw":                 {},
	"repo browse tree":                {},
	"repo cat":                        {},
	"repo comment list":               {},
	"repo compare":                    {},
	"repo default-task list":          {},
	"repo get":                        {},
	"repo label list":                 {},
	"repo list":                       {},
	"repo permissions list":           {},
	"repo permissions show":           {},
	"repo settings auto-decline get":  {},
	"repo settings auto-merge get":    {},
	"repo settings pull-requests get": {},
	"repo settings pull-requests merge-checks list":  {},
	"repo settings security permissions groups list": {},
	"repo settings security permissions users list":  {},
	"repo settings workflow webhooks list":           {},
	"repo ssh-key list":                              {},
	"repo sync status":                               {},
	"reviewer condition list":                        {},
	"reviewer-group list":                            {},
	"reviewer-group users":                           {},
	"search commits":                                 {},
	"search prs":                                     {},
	"search repos":                                   {},
	"ssh-key list":                                   {},
	"tag list":                                       {},
	"tag view":                                       {},
	"webhook get":                                    {},
	"webhook list":                                   {},
	"webhook stats":                                  {},
}

// clientLocalCommands never change anything the user would want previewed:
// they read local configuration, or they honour --dry-run themselves. The
// interceptor lets them run.
var clientLocalCommands = map[string]struct{}{
	"auth alias list":     {},
	"auth git-credential": {},
	"auth server list":    {},

	// update honours --dry-run itself: the flag reaches the workflow, which
	// reports what it would install rather than installing it.
	"update": {},

	// Each prints a completion script and changes nothing.
	"completion bash":       {},
	"completion zsh":        {},
	"completion fish":       {},
	"completion powershell": {},
}

// clientLocalMutatingCommands change state on this machine and do not honour
// --dry-run themselves, so the interceptor previews them instead of letting
// them run.
//
// Each entry names what it writes, because that is the evidence for it being
// here rather than in clientLocalCommands, and the two lists are one edit apart.
var clientLocalMutatingCommands = map[string]dryRunProfile{
	// config.SaveLogin / config.Logout -- the stored credential.
	"auth login":  {Intent: "auth.login", Action: "update", Change: "store credentials"},
	"auth logout": {Intent: "auth.logout", Action: "delete", Change: "remove stored credentials"},

	// config.AddHostAliases / SetHostAliases / RemoveHostAlias / SetDefaultHost.
	"auth alias add":      {Intent: "auth.alias.add", Action: "create", Change: "add host aliases"},
	"auth alias discover": {Intent: "auth.alias.discover", Action: "update", Change: "replace host aliases"},
	"auth alias remove":   {Intent: "auth.alias.remove", Action: "delete", Change: "remove a host alias"},
	"auth server use":     {Intent: "auth.server.use", Action: "update", Change: "change the default host"},

	// The user's git configuration. #571 reproduced this writing 302 bytes into
	// an empty GIT_CONFIG_GLOBAL under --dry-run.
	"auth setup-git": {Intent: "auth.setup-git", Action: "update", Change: "configure git to authenticate through bb"},

	// os.WriteFile, and its removal.
	"ai skill install": {Intent: "ai.skill.install", Action: "create", Change: "write the skill file"},
	"ai skill remove":  {Intent: "ai.skill.remove", Action: "delete", Change: "delete the skill file"},

	// A shell's completion file or a block in its startup file, and their
	// removal.
	"completion install": {Intent: "completion.install", Action: "create", Change: "set shell completion up"},
	"completion remove":  {Intent: "completion.remove", Action: "delete", Change: "take shell completion out"},

	// A working copy, and a local branch.
	"clone":       {Intent: "repo.clone", Action: "create", Change: "clone into a new directory"},
	"repo clone":  {Intent: "repo.clone", Action: "create", Change: "clone into a new directory"},
	"pr checkout": {Intent: "pr.checkout", Action: "create", Change: "create a local branch"},
}

// commandsWithoutDryRun do not take --dry-run, because there is nothing a
// preview of them could say (ADR-096). Passing it is an invalid invocation,
// reported as itself rather than as a verdict on a run that never happens.
var commandsWithoutDryRun = map[string]string{
	// It starts a live, write-capable MCP server: every mutating tool call
	// reaches Bitbucket. It sat among the commands --dry-run lets run, so the
	// one command whose purpose is to hand a machine the ability to mutate
	// Bitbucket was the one where the flag was silently a no-op (#568). A
	// dry run of a long-lived server previews nothing.
	"ai mcp serve": "bb ai mcp serve does not take --dry-run: it starts a live server whose tools reach Bitbucket, " +
		"and a session cannot be previewed. Restrict what the server can do instead: it exposes read-only tools unless --yolo is passed",
}

func classifyCommand(path string) commandClassification {
	trimmed := strings.TrimSpace(path)
	if _, ok := dryRunProfiles[trimmed]; ok {
		return classificationMutating
	}
	if _, ok := readOnlyCommands[trimmed]; ok {
		return classificationReadOnly
	}
	if _, ok := clientLocalCommands[trimmed]; ok {
		return classificationLocal
	}
	if _, ok := clientLocalMutatingCommands[trimmed]; ok {
		return classificationLocalMutating
	}
	if _, ok := commandsWithoutDryRun[trimmed]; ok {
		return classificationWithoutDryRun
	}
	return classificationUnknown
}

func registerGlobalDryRunInterceptors(root *cobra.Command, options *rootOptions) {
	if root == nil || options == nil {
		return
	}

	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		if command == nil {
			return
		}

		path := dryRunCommandPath(command)
		profile, hasDryRunProfile := dryRunProfiles[path]
		if hasDryRunProfile && command.RunE != nil {
			originalRun := command.RunE
			command.RunE = func(cmd *cobra.Command, args []string) error {
				if !options.DryRun {
					return originalRun(cmd, args)
				}

				if profile.Stateful {
					return originalRun(cmd, args)
				}

				preview := newDryRunPreview(profile, cmd, args)
				return writeDryRunPreview(cmd.OutOrStdout(), options.machineOutput(), preview)
			}
		} else if command.RunE != nil {
			originalRun := command.RunE
			command.RunE = func(cmd *cobra.Command, args []string) error {
				if !options.DryRun {
					return originalRun(cmd, args)
				}

				path := dryRunCommandPath(cmd)
				category := classifyCommand(path)
				if category == classificationReadOnly || category == classificationLocal {
					return originalRun(cmd, args)
				}

				// Local state is still the user's state. Preview it rather than
				// changing it and reporting the change in the past tense (#571).
				if localProfile, ok := clientLocalMutatingCommands[path]; ok {
					preview := newDryRunPreview(localProfile, cmd, args)
					return writeDryRunPreview(cmd.OutOrStdout(), options.machineOutput(), preview)
				}

				return dryRunUnsupportedError(path)
			}
		}

		for _, child := range command.Commands() {
			visit(child)
		}
	}

	visit(root)
}

func isServerMutatingPath(path string) bool {
	return classifyCommand(path) == classificationMutating
}

func dryRunCommandPath(command *cobra.Command) string {
	if command == nil {
		return ""
	}

	path := strings.TrimSpace(command.CommandPath())
	path = strings.TrimPrefix(path, "bb ")
	return strings.TrimSpace(path)
}

func dryRunUnsupportedError(path string) error {
	if reason, ok := commandsWithoutDryRun[strings.TrimSpace(path)]; ok {
		// A validation error, not not-implemented: nothing is missing here, the
		// flag does not apply.
		return apperrors.New(apperrors.KindValidation, reason, nil)
	}

	// Every command is classified, and a governance test holds the registries
	// to the tree, so reaching here is a bug in bb: internal, which under
	// --dry-run is a failure to reach a verdict rather than a verdict that the
	// run would fail (ADR-096).
	return apperrors.New(apperrors.KindInternal, fmt.Sprintf("%s has no dry-run classification", path), nil)
}

// newDryRunPreview is the preview of a command whose outcome nothing is checked
// for: the change it would make, predicted.
func newDryRunPreview(profile dryRunProfile, command *cobra.Command, args []string) dryRunPreview {
	target := map[string]any{}
	repository := ""
	if command != nil {
		if flag := command.Flag("repo"); flag != nil {
			repository = strings.TrimSpace(flag.Value.String())
		}
	}
	if repository != "" {
		target["repository"] = repository
	}
	if len(args) > 0 {
		target["args"] = append([]string(nil), args...)
	}

	reason := "nothing is checked first"
	if change := strings.TrimSpace(profile.Change); change != "" {
		reason = fmt.Sprintf("it would %s; nothing is checked first", change)
	}

	return dryrunpreview.New(dryRunItem{
		Intent:          profile.Intent,
		Target:          target,
		Action:          profile.Action,
		PredictedAction: profile.Action,
		Reason:          reason,
		Tier:            dryrunpreview.TierPredicted,
	})
}

func writeDryRunPreview(writer io.Writer, asJSON bool, preview dryRunPreview) error {
	return dryrunpreview.Write(writer, asJSON, preview)
}
