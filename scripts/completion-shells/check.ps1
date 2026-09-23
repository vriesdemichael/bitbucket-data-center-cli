# PowerShell's half of scripts/completion-shells/run.sh.
#
# CompleteInput is the call PSReadLine makes on Tab, so what it returns -- or
# throws -- is what a person at a PowerShell prompt gets. The script Cobra
# generates answered an empty completion with a bare empty string, which
# PowerShell 7 refuses with "the value of argument completionText is null":
# an exception at the prompt on every press that had nothing to offer.
#
# Two things about reading the answer. The script pads each value to the
# longest one when it lays candidates out in columns, so values are compared
# trimmed. And a pipeline that yields one item yields the item, not a list of
# one, so every answer is wrapped in @() before it is counted or indexed.
$ErrorActionPreference = 'Stop'

bb completion powershell | Out-String | Invoke-Expression

$failures = 0

function Complete([string] $line) {
    [System.Management.Automation.CommandCompletion]::CompleteInput($line, $line.Length, $null)
}

function Fail([string] $case, [string] $problem) {
    Write-Output ("FAIL  pwsh  {0}: {1}" -f $case, $problem)
    $script:failures++
}

# An enum flag completes the values it validates against.
$values = @((Complete 'bb pr list --state ').CompletionMatches | ForEach-Object { $_.CompletionText.Trim() })
foreach ($wanted in 'open', 'closed', 'all') {
    if ($values -notcontains $wanted) { Fail 'enum values' "expected $wanted, got: $($values -join ', ')" }
}

# Each value describes itself. Where the description lands depends on the
# script's layout -- the tooltip, or folded into the listed text -- so it is
# looked for in both.
$levels = @((Complete 'bb --log-level ').CompletionMatches)
$described = $levels | Where-Object { "$($_.ToolTip) $($_.ListItemText) $($_.CompletionText)" -match 'failures only' }
if (-not $described) {
    Fail 'distinct descriptions' "expected a level described as 'failures only', got: $($levels.ToolTip -join ' | ')"
}

# Nothing to offer, with a word typed: the word comes back unchanged, and there
# is no exception. This is the case that threw.
try {
    $answer = @((Complete 'bb pr merge --repo PRJ/nothing zz').CompletionMatches | ForEach-Object { $_.CompletionText.Trim() })
    if ($answer.Count -ne 1 -or $answer[0] -ne 'zz') {
        Fail 'no candidates, no files' "expected the typed word back alone, got: $($answer -join ', ')"
    }
} catch {
    Fail 'no candidates, no files' "threw: $($_.Exception.Message)"
}

# Nothing to offer and nothing typed. PowerShell accepts no value that would
# leave the line alone, so it lists the directory itself; what must not happen
# is the exception.
try {
    $null = Complete 'bb pr merge --repo PRJ/nothing '
} catch {
    Fail 'nothing typed' "threw: $($_.Exception.Message)"
}

if ($failures -gt 0) { exit 1 }
