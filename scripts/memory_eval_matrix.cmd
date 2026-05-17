@echo off
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0memory_eval_matrix.ps1" %*
exit /b %ERRORLEVEL%
