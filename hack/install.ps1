$ErrorActionPreference = "Stop"

$Repo = "cloudygreybeard/authzer"
$Binary = "authzer"

if ($env:AUTHZER_INSTALL_DIR) {
    $InstallDir = $env:AUTHZER_INSTALL_DIR
} else {
    $InstallDir = Join-Path $HOME "bin"
}

$Arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    "X64"  { "amd64" }
    "Arm64" { "arm64" }
    default { Write-Error "Unsupported architecture: $_"; exit 1 }
}

if ($env:AUTHZER_VERSION) {
    $Version = $env:AUTHZER_VERSION
} else {
    $Release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -UseBasicParsing
    $Version = $Release.tag_name -replace '^v', ''
    if (-not $Version) {
        Write-Error "Failed to determine latest version"
        exit 1
    }
}

$ZipName = "${Binary}_${Version}_windows_${Arch}.zip"
$Url = "https://github.com/$Repo/releases/download/v${Version}/$ZipName"

Write-Host "Installing $Binary v$Version (windows/$Arch)..."

$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

try {
    $ZipPath = Join-Path $TmpDir $ZipName
    Invoke-WebRequest -Uri $Url -OutFile $ZipPath -UseBasicParsing

    Expand-Archive -Path $ZipPath -DestinationPath $TmpDir -Force

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    $Src = Join-Path $TmpDir "$Binary.exe"
    $Dst = Join-Path $InstallDir "$Binary.exe"
    Move-Item -Path $Src -Destination $Dst -Force

    Write-Host "Installed $Dst (v$Version)"
    Write-Host ""

    $UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($UserPath -notlike "*$InstallDir*") {
        [Environment]::SetEnvironmentVariable("PATH", "$InstallDir;$UserPath", "User")
        $env:PATH = "$InstallDir;$env:PATH"
        Write-Host "Added $InstallDir to your PATH."
        Write-Host ""
    }

    Write-Host "Next steps:"
    Write-Host "  authzer doctor    # check setup"
    Write-Host "  authzer launch    # start browser with CDP"
    Write-Host "  authzer --help"
} finally {
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}
