# ==========================================
# 1. 稳健的环境变量注入器
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
# 2. 核心 CLI 工具别名
# ==========================================
if (Get-Command lazygit -ErrorAction SilentlyContinue) {
    Set-Alias lg lazygit
}

# ==========================================
# 3. 自动化开发工作流函数 (全场景智能版)
# ==========================================
function gpa {
    # 拦截 1：确认 Git 仓库
    if (-not (git rev-parse --is-inside-work-tree 2>$null)) {
        Write-Warning "当前目录不是 Git 仓库，终止执行。"
        return
    }

    # 拦截 2：确认不在主干分支
    $currentBranch = git rev-parse --abbrev-ref HEAD
    if ($currentBranch -eq "main" -or $currentBranch -eq "develop" -or $currentBranch -eq "master") {
        Write-Host ">>> [拦截] 禁止在主干分支 ($currentBranch) 上直接运行！请先切换至功能分支。" -ForegroundColor Red
        return
    }

    # ==========================================
    # 动态测试嗅探执行器
    # ==========================================
    Write-Host ">>> [1/4] 嗅探项目类型并执行本地质量检查..." -ForegroundColor Cyan
    $testFailed = $false

    if (Test-Path "go.mod") {
        Write-Host "  -> 检测到 Go 项目，运行 go test..." -ForegroundColor DarkGray
        go test -count=1 ./...
        if ($LASTEXITCODE -ne 0) { $testFailed = $true }
    }
    elseif (Test-Path "package.json") {
        Write-Host "  -> 检测到前端/Node 项目，跳过自动测试（如需请在配置中取消注释 npm test）" -ForegroundColor DarkGray
        # npm run test; if ($LASTEXITCODE -ne 0) { $testFailed = $true }
    }
    elseif (Test-Path "Cargo.toml") {
        Write-Host "  -> 检测到 Rust 项目，运行 cargo test..." -ForegroundColor DarkGray
        cargo test
        if ($LASTEXITCODE -ne 0) { $testFailed = $true }
    }
    else {
        Write-Host "  -> 未检测到已知测试框架，跳过本地测试步骤。" -ForegroundColor DarkGray
    }

    if ($testFailed) {
        Write-Host ">>> [拦截] 本地测试未通过，请修复代码！" -ForegroundColor Red
        return
    }

    # ==========================================
    # Commit 数量探测与合并策略选择
    # ==========================================
    # 获取当前分支相对于 main/develop/master 的新增 commit 数量
    $baseBranch = "main"
    if (-not (git show-ref --verify --quiet refs/heads/main)) {
        $baseBranch = "develop"
        if (-not (git show-ref --verify --quiet refs/heads/develop)) { $baseBranch = "master" }
    }

    $commitCountStr = git rev-list --count "$currentBranch" "^$baseBranch" 2>$null
    [int]$commitCount = if ($commitCountStr) { $commitCountStr } else { 0 }
    $strategyFlag = ""

    if ($commitCount -le 1) {
        Write-Host ">>> 检测到当前分支新增 $commitCount 个提交，自动采用 Squash 压缩策略..." -ForegroundColor Cyan
        $strategyFlag = "--squash"
    } else {
        $title = "⚠️ 发现多个 Commit ($commitCount 个)"
        $message = "请选择合入 [$baseBranch] 的策略："

        $squashDesc = New-Object System.Management.Automation.Host.ChoiceDescription "&Squash", "合并为 1 个提交 (主干整洁，丢弃过程)"
        $rebaseDesc = New-Object System.Management.Automation.Host.ChoiceDescription "&Rebase", "保留全部 $commitCount 个提交 (主干冗长，精准溯源)"
        $cancelDesc = New-Object System.Management.Automation.Host.ChoiceDescription "&Cancel", "取消本次提交"

        $options = [System.Management.Automation.Host.ChoiceDescription[]]($squashDesc, $rebaseDesc, $cancelDesc)
        $result = $host.ui.PromptForChoice($title, $message, $options, 0)

        if ($result -eq 0) {
            $strategyFlag = "--squash"
            Write-Host ">>> 已选择 Squash 模式。" -ForegroundColor Cyan
        } elseif ($result -eq 1) {
            $strategyFlag = "--rebase"
            Write-Host ">>> 已选择 Rebase 模式。" -ForegroundColor Cyan
        } else {
            Write-Host ">>> 操作取消。" -ForegroundColor Yellow
            return
        }
    }

    # ==========================================
    # 远端同步与 PR 创建
    # ==========================================
    Write-Host ">>> [2/4] 推送代码至远端分支 ($currentBranch)..." -ForegroundColor Cyan
    git push origin $currentBranch
    if ($LASTEXITCODE -ne 0) {
        Write-Host ">>> [拦截] 推送失败，请检查网络或远端冲突。" -ForegroundColor Red
        return
    }

    Write-Host ">>> [3/4] 静默创建 Pull Request..." -ForegroundColor Cyan
    gh pr create --fill
    if ($LASTEXITCODE -ne 0) {
        Write-Warning ">>> PR 创建失败。可能远端无变更或 PR 已存在。"
        return
    }

    Write-Host ">>> [4/4] 挂载自动合并钩子 ($strategyFlag)..." -ForegroundColor Cyan
    gh pr merge --auto $strategyFlag --delete-branch

    if ($LASTEXITCODE -eq 0) {
        Write-Host ">>> [完成] 自动化流水线已触发。等待 CI 通过后将自动合入 $baseBranch 并清理分支。" -ForegroundColor Green
    } else {
        Write-Warning ">>> 自动合并挂载失败。请检查 GitHub 仓库设置是否允许 auto-merge。"
    }
}
