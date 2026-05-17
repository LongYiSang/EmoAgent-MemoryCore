@echo off
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0memory_eval_quality.ps1" %*
exit /b %ERRORLEVEL%
