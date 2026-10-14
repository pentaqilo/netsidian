# Native Windows build. Requires Python 3, Node.js/npm and Go.
$ErrorActionPreference = 'Stop'
$builder = Join-Path $PSScriptRoot 'scripts/build-desktop.py'
if (Get-Command py -ErrorAction SilentlyContinue) {
    & py -3 $builder --platform windows @args
} elseif (Get-Command python3 -ErrorAction SilentlyContinue) {
    & python3 $builder --platform windows @args
} elseif (Get-Command python -ErrorAction SilentlyContinue) {
    & python $builder --platform windows @args
} else {
    throw 'Python 3 is required'
}
exit $LASTEXITCODE
