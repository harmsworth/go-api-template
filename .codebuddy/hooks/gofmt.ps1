# PostToolUse hook: 对本次写入/编辑的 .go 文件执行 gofmt -w。
# 约定：从 stdin 读取 hook 的 JSON，成功时静默退出 0，任何异常都不阻塞编辑。
# 兼容 IDE 与 CLI 两种工具参数命名：filePath / file_path / path / target_file。

param(
    # 仅用于本地测试：直接传入 hook JSON；留空时从 stdin 读取。
    [string]$HookPayloadJson
)

$ErrorActionPreference = 'Stop'

function Exit-Silently {
    exit 0
}

try {
    if ([string]::IsNullOrWhiteSpace($HookPayloadJson)) {
        try {
            [Console]::InputEncoding = New-Object System.Text.UTF8Encoding $false
        } catch {
            # 某些宿主不支持重设编码，忽略即可。
        }
        $raw = [Console]::In.ReadToEnd()
    } else {
        $raw = $HookPayloadJson
    }
    if ([string]::IsNullOrWhiteSpace($raw)) { Exit-Silently }

    $payload = $raw | ConvertFrom-Json -ErrorAction Stop
    if ($null -eq $payload -or $null -eq $payload.tool_input) { Exit-Silently }

    $toolInput = $payload.tool_input
    $candidates = @($toolInput.filePath, $toolInput.file_path, $toolInput.path, $toolInput.target_file)
    $file = $candidates | Where-Object { $_ -is [string] -and $_.Trim() -ne '' } | Select-Object -First 1

    if ([string]::IsNullOrWhiteSpace($file)) { Exit-Silently }
    if ([System.IO.Path]::GetExtension($file) -ne '.go') { Exit-Silently }

    # 相对路径按项目根目录解析，避免 hook 进程 cwd 与工作区不一致。
    if (-not (Test-Path -LiteralPath $file -PathType Leaf) -and
        -not [string]::IsNullOrWhiteSpace($env:CODEBUDDY_PROJECT_DIR)) {
        $candidate = Join-Path $env:CODEBUDDY_PROJECT_DIR $file
        if (Test-Path -LiteralPath $candidate -PathType Leaf) { $file = $candidate }
    }
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { Exit-Silently }

    $gofmt = Get-Command gofmt -ErrorAction SilentlyContinue
    if ($null -eq $gofmt) {
        [Console]::Error.WriteLine("[gofmt hook] gofmt 不在 PATH 中，跳过 $file")
        Exit-Silently
    }

    $output = & $gofmt.Source -w $file 2>&1
    if ($LASTEXITCODE -ne 0) {
        [Console]::Error.WriteLine("[gofmt hook] $file 格式化失败: $output")
        exit 1
    }

    Exit-Silently
}
catch {
    [Console]::Error.WriteLine("[gofmt hook] 跳过: $($_.Exception.Message)")
    Exit-Silently
}
