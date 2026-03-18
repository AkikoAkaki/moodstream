# ==========================================
# 1. Path injection
# ==========================================
function Add-ToPath {
    param([string]$PathToAdd)
    if ((Test-Path $PathToAdd) -and ($env:Path -split ';' -notcontains $PathToAdd)) {
        $env:Path += ";$PathToAdd"
    }
}

Add-ToPath "C:\Program Files\GitHub CLI"
Add-ToPath "C:\Program Files\Go\bin"
Add-ToPath "$env:USERPROFILE\go\bin"

# ==========================================
# 2. Aliases
# ==========================================
if (Get-Command lazygit -ErrorAction SilentlyContinue) {
    Set-Alias lg lazygit
}

# ==========================================
# 3. Automated workflow
# ==========================================
function gpa {
    if (-not (git rev-parse --is-inside-work-tree 2>$null)) {
        Write-Warning "Current directory is not a Git repository."
        return
    }

    $currentBranch = (git rev-parse --abbrev-ref HEAD).Trim()
    if ($currentBranch -in @("main", "develop", "master")) {
        Write-Host ">>> [BLOCKED] Do not run on protected branch: $currentBranch" -ForegroundColor Red
        return
    }

    Write-Host ">>> [1/4] Detecting project type and running local checks..." -ForegroundColor Cyan
    $testFailed = $false

    if (Test-Path "go.mod") {
        Write-Host "  -> Go project detected: go test -count=1 ./..." -ForegroundColor DarkGray
        go test -count=1 ./...
        if ($LASTEXITCODE -ne 0) { $testFailed = $true }
    }
    elseif (Test-Path "package.json") {
        Write-Host "  -> Node project detected: test step skipped by config" -ForegroundColor DarkGray
        # npm run test; if ($LASTEXITCODE -ne 0) { $testFailed = $true }
    }
    elseif (Test-Path "Cargo.toml") {
        Write-Host "  -> Rust project detected: cargo test" -ForegroundColor DarkGray
        cargo test
        if ($LASTEXITCODE -ne 0) { $testFailed = $true }
    }
    else {
        Write-Host "  -> Unknown project type: local test step skipped" -ForegroundColor DarkGray
    }

    if ($testFailed) {
        Write-Host ">>> [BLOCKED] Local tests failed. Fix before pushing." -ForegroundColor Red
        return
    }

    # Prefer remote branches for correctness.
    # Prefer remote branches for correctness.
    $baseBranch = $null
    foreach ($candidate in @("main", "develop", "master")) {
        git show-ref --verify --quiet "refs/remotes/origin/$candidate"
        if ($LASTEXITCODE -eq 0) {
            $baseBranch = $candidate
            break
        }
    }

    if (-not $baseBranch) {
        foreach ($candidate in @("main", "develop", "master")) {
            git show-ref --verify --quiet "refs/heads/$candidate"
            if ($LASTEXITCODE -eq 0) {
                $baseBranch = $candidate
                break
            }
        }
    }

    if (-not $baseBranch) {
        Write-Host ">>> [BLOCKED] Cannot determine base branch (main/develop/master)." -ForegroundColor Red
        return
    }

    git fetch origin $baseBranch --quiet

    $commitCountStr = git rev-list --count "HEAD" "^origin/$baseBranch" 2>$null
    [int]$commitCount = if ($commitCountStr) { $commitCountStr } else { 0 }

    $strategyFlag = ""
    if ($commitCount -le 1) {
        Write-Host ">>> Detected $commitCount new commit(s), using squash strategy." -ForegroundColor Cyan
        $strategyFlag = "--squash"
    }
    else {
        $title = "Multiple commits detected ($commitCount)"
        $message = "Select merge strategy into [$baseBranch]:"

        $squashDesc = New-Object System.Management.Automation.Host.ChoiceDescription "&Squash", "Merge into one commit"
        $rebaseDesc = New-Object System.Management.Automation.Host.ChoiceDescription "&Rebase", "Keep all commits"
        $cancelDesc = New-Object System.Management.Automation.Host.ChoiceDescription "&Cancel", "Cancel operation"

        $options = [System.Management.Automation.Host.ChoiceDescription[]]($squashDesc, $rebaseDesc, $cancelDesc)
        $result = $host.ui.PromptForChoice($title, $message, $options, 0)

        if ($result -eq 0) {
            $strategyFlag = "--squash"
            Write-Host ">>> Strategy selected: squash" -ForegroundColor Cyan
        }
        elseif ($result -eq 1) {
            $strategyFlag = "--rebase"
            Write-Host ">>> Strategy selected: rebase" -ForegroundColor Cyan
        }
        else {
            Write-Host ">>> Operation cancelled." -ForegroundColor Yellow
            return
        }
    }

    Write-Host ">>> [2/4] Pushing branch: $currentBranch" -ForegroundColor Cyan
    git push origin $currentBranch
    if ($LASTEXITCODE -ne 0) {
        Write-Host ">>> [BLOCKED] Push failed." -ForegroundColor Red
        return
    }

    Write-Host ">>> [3/4] Creating or resolving Pull Request..." -ForegroundColor Cyan
    gh pr create --fill --base $baseBranch 1>$null 2>$null

    $prUrl = (gh pr view --json url --jq .url 2>$null)
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($prUrl)) {
        Write-Warning ">>> Could not resolve PR URL for current branch."
        return
    }

    $prUrl = $prUrl.Trim()
    Write-Host "  -> PR: $prUrl" -ForegroundColor DarkGray

    Write-Host ">>> [4/4] Attaching auto-merge hook ($strategyFlag)..." -ForegroundColor Cyan
    gh pr merge $prUrl --auto $strategyFlag --delete-branch

    if ($LASTEXITCODE -eq 0) {
        Write-Host ">>> [DONE] Auto-merge attached. CI pass will merge into $baseBranch and delete branch." -ForegroundColor Green
    }
    else {
        Write-Warning ">>> Auto-merge attach failed. Check repo auto-merge setting and branch protection rules."
    }
}
