using System.Diagnostics;
using System.IO.Compression;
using System.Security.Cryptography;
using System.Text;
using System.Text.RegularExpressions;
using System.Windows.Forms;

namespace DTVUpdater;

internal static class Program
{
    private const string ExecutableName = "DTV.exe";
    private const long MaximumArchiveBytes = 3L * 1024 * 1024 * 1024;
    private static string LogPath => Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
        "D TV", "updater.log");

    [STAThread]
    private static async Task<int> Main(string[] args)
    {
        if (args.Contains("--self-test", StringComparer.Ordinal))
            return RunSelfTest();

        Options options;
        try
        {
            options = Options.Parse(args);
        }
        catch (Exception ex)
        {
            ShowError("更新參數無效：" + ex.Message);
            return 2;
        }

        Directory.CreateDirectory(Path.GetDirectoryName(LogPath)!);
        Log("Standalone updater started.");
        try
        {
            await ApplyUpdateAsync(options);
            return 0;
        }
        catch (Exception ex)
        {
            Log("UPDATE FAILED: " + ex);
            ShowError(
                "D TV 更新未完成。舊版程式已盡力還原；請先保留備份資料夾，並查看更新紀錄。"
                + Environment.NewLine + Environment.NewLine
                + ex.Message
                + Environment.NewLine + Environment.NewLine
                + "更新紀錄：" + LogPath);
            TryOpenRelease(options.ReleaseUrl);
            return 1;
        }
    }

    private static int RunSelfTest()
    {
        var cases = new (string Name, bool Expected)[]
        {
            ("DTV.exe", true),
            ("data/icudtl.dat", true),
            ("../outside.exe", false),
            ("folder/../../outside.exe", false),
            ("C:/outside.exe", false),
            ("//server/share/file", false),
            ("\\\\server\\share\\file", false),
            ("", false),
        };
        foreach (var item in cases)
        {
            if (IsSafeEntryName(item.Name) != item.Expected)
            {
                ShowError("Updater self-test failed: " + item.Name);
                return 10;
            }
        }
        return Regex.IsMatch("1.0.8", @"^\d+\.\d+\.\d+$") ? 0 : 11;
    }

    private static async Task ApplyUpdateAsync(Options options)
    {
        if (!File.Exists(options.ArchivePath))
            throw new FileNotFoundException("找不到已下載的 Windows 更新 ZIP。", options.ArchivePath);

        var actualDigest = await ComputeSha256Async(options.ArchivePath);
        if (!CryptographicOperations.FixedTimeEquals(
                Convert.FromHexString(actualDigest),
                Convert.FromHexString(options.ArchiveSha256)))
            throw new InvalidDataException("Windows 更新 ZIP 的 SHA-256 不符，已取消更新。");

        if (!Directory.Exists(options.InstallDirectory))
            throw new DirectoryNotFoundException("找不到 D TV 安裝資料夾：" + options.InstallDirectory);

        var executable = Path.Combine(options.InstallDirectory, ExecutableName);
        if (!File.Exists(executable))
            throw new FileNotFoundException("找不到目前安裝的 DTV.exe。", executable);

        var probe = Path.Combine(options.InstallDirectory, ".dtv-update-write-" + Guid.NewGuid().ToString("N") + ".tmp");
        try
        {
            await File.WriteAllTextAsync(probe, "D TV update permission check");
            File.Delete(probe);
        }
        catch (Exception ex)
        {
            TryDeleteFile(probe);
            throw new UnauthorizedAccessException(
                "D TV 安裝資料夾沒有寫入權限。請將免安裝版解壓到目前使用者可寫入的位置，再重新更新。",
                ex);
        }

        var workDirectory = Path.Combine(Path.GetTempPath(), "D TV Updater", Guid.NewGuid().ToString("N"));
        var stageDirectory = Path.Combine(workDirectory, "package");
        var backupDirectory = Path.Combine(workDirectory, "backup");
        var ackDirectory = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
            "D TV", "UpdaterHealth");
        var ackPath = Path.Combine(ackDirectory, Guid.NewGuid().ToString("N") + ".ack");
        var token = Guid.NewGuid().ToString("N");
        var changedFiles = new List<string>();
        var createdFiles = new List<string>();
        Process? newProcess = null;
        var installStarted = false;
        var success = false;
        var rollbackComplete = true;

        Directory.CreateDirectory(stageDirectory);
        Directory.CreateDirectory(ackDirectory);
        TryDeleteFile(ackPath);

        try
        {
            ExtractAndValidateArchive(options.ArchivePath, stageDirectory);
            Log("Archive verified and extracted.");
            await WaitForOldProcessAsync(options.ProcessId);
            Log("Previous D TV process has exited.");

            var stagedFiles = Directory.GetFiles(stageDirectory, "*", SearchOption.AllDirectories);
            if (stagedFiles.Length == 0)
                throw new InvalidDataException("更新 ZIP 沒有可安裝的檔案。");

            // Back up every destination before modifying any installed file.
            foreach (var source in stagedFiles)
            {
                var relative = Path.GetRelativePath(stageDirectory, source);
                var destination = Path.Combine(options.InstallDirectory, relative);
                if (File.Exists(destination))
                {
                    var backup = Path.Combine(backupDirectory, relative);
                    Directory.CreateDirectory(Path.GetDirectoryName(backup)!);
                    File.Copy(destination, backup, overwrite: true);
                    changedFiles.Add(relative);
                }
                else if (Directory.Exists(destination))
                {
                    throw new IOException("安裝目標與資料夾名稱衝突：" + relative);
                }
                else
                {
                    createdFiles.Add(relative);
                }
            }

            installStarted = true;
            foreach (var source in stagedFiles)
            {
                var relative = Path.GetRelativePath(stageDirectory, source);
                var destination = Path.Combine(options.InstallDirectory, relative);
                Directory.CreateDirectory(Path.GetDirectoryName(destination)!);
                File.Copy(source, destination, overwrite: true);
            }

            ValidateInstalledFiles(options.InstallDirectory);
            Log("Files replaced; starting the new app for version handshake.");

            var startInfo = new ProcessStartInfo(executable)
            {
                WorkingDirectory = options.InstallDirectory,
                UseShellExecute = true,
            };
            startInfo.ArgumentList.Add("--dtv-updater-ack=" + ackPath);
            startInfo.ArgumentList.Add("--dtv-updater-version=" + options.ExpectedVersion);
            startInfo.ArgumentList.Add("--dtv-updater-token=" + token);
            newProcess = Process.Start(startInfo)
                ?? throw new InvalidOperationException("無法啟動更新後的 D TV。");

            var deadline = DateTime.UtcNow.AddSeconds(90);
            while (DateTime.UtcNow < deadline)
            {
                if (File.Exists(ackPath))
                {
                    var lines = await File.ReadAllLinesAsync(ackPath);
                    if (lines.Length >= 2
                        && string.Equals(lines[0].Trim(), options.ExpectedVersion, StringComparison.Ordinal)
                        && string.Equals(lines[1].Trim(), token, StringComparison.Ordinal))
                    {
                        success = true;
                        Log("New D TV process acknowledged version " + options.ExpectedVersion + ".");
                        break;
                    }
                }

                newProcess.Refresh();
                if (newProcess.HasExited)
                    throw new InvalidOperationException("新版 D TV 啟動後立即結束，結束代碼：" + newProcess.ExitCode);

                await Task.Delay(300);
            }

            if (!success)
                throw new TimeoutException("新版 D TV 未在 90 秒內回報啟動版本，已取消更新並還原舊版。");

            TryDeleteFile(ackPath);
            TryDeleteFile(options.ArchivePath);
            TryDeleteEmptyParentDirectory(options.ArchivePath);
            Log("Update completed; removed the downloaded ZIP.");
        }
        catch (Exception updateError)
        {
            Log("Update transaction failed: " + updateError.Message);
            if (newProcess is not null)
            {
                try
                {
                    if (!newProcess.HasExited)
                    {
                        newProcess.Kill(entireProcessTree: true);
                        await newProcess.WaitForExitAsync();
                    }
                }
                catch (Exception ex)
                {
                    Log("Could not stop the new process: " + ex.Message);
                }
            }

            if (installStarted)
            {
                foreach (var relative in changedFiles)
                {
                    try
                    {
                        var backup = Path.Combine(backupDirectory, relative);
                        var destination = Path.Combine(options.InstallDirectory, relative);
                        if (File.Exists(backup))
                        {
                            Directory.CreateDirectory(Path.GetDirectoryName(destination)!);
                            File.Copy(backup, destination, overwrite: true);
                        }
                        else
                        {
                            rollbackComplete = false;
                            Log("Missing backup for " + relative);
                        }
                    }
                    catch (Exception ex)
                    {
                        rollbackComplete = false;
                        Log("ROLLBACK FAILED " + relative + ": " + ex.Message);
                    }
                }

                foreach (var relative in createdFiles)
                {
                    try
                    {
                        TryDeleteFile(Path.Combine(options.InstallDirectory, relative));
                    }
                    catch (Exception ex)
                    {
                        rollbackComplete = false;
                        Log("NEW FILE CLEANUP FAILED " + relative + ": " + ex.Message);
                    }
                }
            }

            if (!IsProcessRunning(options.ProcessId) && installStarted)
            {
                try
                {
                    Process.Start(new ProcessStartInfo(executable)
                    {
                        WorkingDirectory = options.InstallDirectory,
                        UseShellExecute = true,
                    });
                    Log("Restarted the restored previous version.");
                }
                catch (Exception ex)
                {
                    rollbackComplete = false;
                    Log("Could not restart the previous version: " + ex.Message);
                }
            }

            if (!rollbackComplete)
            {
                throw new InvalidOperationException(
                    updateError.Message + Environment.NewLine
                    + "部分檔案還原未完成，請保留備份資料夾：" + backupDirectory,
                    updateError);
            }
            throw;
        }
        finally
        {
            TryDeleteFile(ackPath);
            if (success || rollbackComplete)
            {
                try
                {
                    if (Directory.Exists(workDirectory))
                        Directory.Delete(workDirectory, recursive: true);
                }
                catch (Exception ex)
                {
                    Log("Temporary folder cleanup warning: " + ex.Message);
                }
            }
            else
            {
                Log("Recovery files retained at " + workDirectory);
            }
        }
    }

    private static async Task WaitForOldProcessAsync(int processId)
    {
        Process? oldProcess;
        try { oldProcess = Process.GetProcessById(processId); }
        catch (ArgumentException) { return; }

        using (oldProcess)
        using (var timeout = new CancellationTokenSource(TimeSpan.FromMinutes(3)))
        {
            try { await oldProcess.WaitForExitAsync(timeout.Token); }
            catch (OperationCanceledException)
            {
                throw new TimeoutException("等待 D TV 關閉逾時；尚未修改任何安裝檔案。");
            }
        }
    }

    private static bool IsProcessRunning(int processId)
    {
        try
        {
            using var process = Process.GetProcessById(processId);
            return !process.HasExited;
        }
        catch { return false; }
    }

    private static void ExtractAndValidateArchive(string archivePath, string stageDirectory)
    {
        using var archive = ZipFile.OpenRead(archivePath);
        var seenPaths = new HashSet<string>(StringComparer.OrdinalIgnoreCase);
        long totalBytes = 0;

        foreach (var entry in archive.Entries)
        {
            if (!IsSafeEntryName(entry.FullName))
                throw new InvalidDataException("更新 ZIP 含有不安全的檔案路徑：" + entry.FullName);

            var normalized = entry.FullName.Replace('/', Path.DirectorySeparatorChar);
            var destination = Path.GetFullPath(Path.Combine(stageDirectory, normalized));
            var stagePrefix = Path.GetFullPath(stageDirectory).TrimEnd(Path.DirectorySeparatorChar) + Path.DirectorySeparatorChar;
            if (!destination.StartsWith(stagePrefix, StringComparison.OrdinalIgnoreCase)
                && !string.Equals(destination, Path.GetFullPath(stageDirectory), StringComparison.OrdinalIgnoreCase))
                throw new InvalidDataException("更新 ZIP 路徑超出暫存資料夾：" + entry.FullName);

            if (!seenPaths.Add(normalized.TrimEnd(Path.DirectorySeparatorChar)))
                throw new InvalidDataException("更新 ZIP 含有重複檔案路徑：" + entry.FullName);

            var unixMode = (entry.ExternalAttributes >> 16) & 0xF000;
            if (unixMode == 0xA000)
                throw new InvalidDataException("更新 ZIP 不允許符號連結：" + entry.FullName);

            if (entry.FullName.EndsWith("/", StringComparison.Ordinal))
            {
                Directory.CreateDirectory(destination);
                continue;
            }

            totalBytes += entry.Length;
            if (totalBytes > MaximumArchiveBytes)
                throw new InvalidDataException("更新 ZIP 解壓縮後超過允許大小。");

            Directory.CreateDirectory(Path.GetDirectoryName(destination)!);
            using var input = entry.Open();
            using var output = new FileStream(destination, FileMode.CreateNew, FileAccess.Write, FileShare.None);
            input.CopyTo(output);
            if (output.Length != entry.Length)
                throw new InvalidDataException("更新 ZIP 解壓縮長度不符：" + entry.FullName);
        }

        ValidateInstalledFiles(stageDirectory);
    }

    private static void ValidateInstalledFiles(string directory)
    {
        string[] required =
        [
            "DTV.exe",
            "DTV-Updater.exe",
            "duanju_core.dll",
            "flutter_windows.dll",
            "libffmpegkit.dll",
            "libmpv-2.dll",
            "msvcp140.dll",
            "vcruntime140.dll",
            Path.Combine("data", "icudtl.dat"),
            Path.Combine("data", "app.so"),
        ];

        var missing = required.Where(name => !File.Exists(Path.Combine(directory, name))).ToArray();
        if (missing.Length > 0)
            throw new InvalidDataException("Windows 更新套件缺少必要檔案：" + string.Join(", ", missing));
    }

    private static bool IsSafeEntryName(string name)
    {
        if (string.IsNullOrWhiteSpace(name) || name.Contains('\\'))
            return false;
        if (Path.IsPathRooted(name)
            || name.StartsWith("/", StringComparison.Ordinal)
            || Regex.IsMatch(name, @"^[A-Za-z]:"))
            return false;
        return !name.Split('/').Any(segment => segment is "." or "..");
    }

    private static async Task<string> ComputeSha256Async(string path)
    {
        await using var stream = File.OpenRead(path);
        return Convert.ToHexString(await SHA256.HashDataAsync(stream)).ToLowerInvariant();
    }

    private static void TryDeleteFile(string path)
    {
        try { if (File.Exists(path)) File.Delete(path); }
        catch (Exception ex) { Log("File cleanup warning (" + path + "): " + ex.Message); }
    }

    private static void TryDeleteEmptyParentDirectory(string filePath)
    {
        try
        {
            var directory = Path.GetDirectoryName(filePath);
            if (directory is not null && Directory.Exists(directory)
                && !Directory.EnumerateFileSystemEntries(directory).Any())
                Directory.Delete(directory);
        }
        catch (Exception ex) { Log("Download directory cleanup warning: " + ex.Message); }
    }

    private static void TryOpenRelease(string url)
    {
        try { Process.Start(new ProcessStartInfo(url) { UseShellExecute = true }); }
        catch (Exception ex) { Log("Could not open release page: " + ex.Message); }
    }

    private static void ShowError(string message)
    {
        try
        {
            MessageBox.Show(message, "D TV 更新", MessageBoxButtons.OK, MessageBoxIcon.Warning);
        }
        catch { }
    }

    private static void Log(string message)
    {
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(LogPath)!);
            File.AppendAllText(
                LogPath,
                $"[{DateTime.Now:yyyy-MM-dd HH:mm:ss}] {message}{Environment.NewLine}",
                new UTF8Encoding(false));
        }
        catch { }
    }

    private sealed record Options(
        int ProcessId,
        string ArchivePath,
        string InstallDirectory,
        string ExpectedVersion,
        string ArchiveSha256,
        string ReleaseUrl)
    {
        public static Options Parse(string[] args)
        {
            var values = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
            foreach (var argument in args)
            {
                if (!argument.StartsWith("--", StringComparison.Ordinal))
                    throw new ArgumentException("偵測到不支援的命令列參數。");
                var separator = argument.IndexOf('=');
                if (separator < 3)
                    throw new ArgumentException("參數必須使用 --名稱=值 格式。");
                if (!values.TryAdd(argument[..separator], argument[(separator + 1)..]))
                    throw new ArgumentException("重複的參數：" + argument[..separator]);
            }

            string Required(string key) =>
                values.TryGetValue(key, out var value) && !string.IsNullOrWhiteSpace(value)
                    ? value : throw new ArgumentException("缺少必要參數：" + key);

            if (!int.TryParse(Required("--process-id"), out var processId) || processId <= 0)
                throw new ArgumentException("D TV 程序識別碼無效.");

            var archive = Path.GetFullPath(Required("--archive-path"));
            var installDirectory = Path.GetFullPath(Required("--install-directory"));
            if (!string.Equals(Required("--executable-name"), ExecutableName, StringComparison.OrdinalIgnoreCase))
                throw new ArgumentException("不支援的應用程式檔名。");

            var version = Required("--expected-version").TrimStart('v', 'V');
            if (!Regex.IsMatch(version, @"^\d+\.\d+\.\d+$"))
                throw new ArgumentException("新版版本號格式無效。");

            var digest = Required("--archive-sha256").ToLowerInvariant();
            if (!Regex.IsMatch(digest, @"^[0-9a-f]{64}$"))
                throw new ArgumentException("ZIP SHA-256 格式無效。");

            var releaseUrl = Required("--release-url");
            if (!Uri.TryCreate(releaseUrl, UriKind.Absolute, out var releaseUri)
                || releaseUri.Scheme != Uri.UriSchemeHttps
                || releaseUri.Host != "github.com"
                || !releaseUri.AbsolutePath.StartsWith("/DickyR15/D-TV/releases/tag/", StringComparison.OrdinalIgnoreCase))
                throw new ArgumentException("GitHub Release 網址無效。");

            return new Options(processId, archive, installDirectory, version, digest, releaseUri.AbsoluteUri);
        }
    }
}
