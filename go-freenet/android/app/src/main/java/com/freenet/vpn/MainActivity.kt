package com.freenet.vpn

import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Main screen — single-activity Compose UI.
 *
 * Layout:
 *   ┌─────────────────────────┐
 *   │  FreeNet                │  ← top bar
 *   │                         │
 *   │   ●  ВКЛЮЧИТЬ  ●        │  ← big round button (centre)
 *   │                         │
 *   │  Стратегия: [auto ▾]    │  ← strategy dropdown
 *   │                         │
 *   │  ── Статистика ──       │
 *   │  Соединений: 12         │
 *   │  Байт входящих: 1.2 МБ  │
 *   │                         │
 *   │  ── Лог ──              │
 *   │  [scrollable log]       │
 *   └─────────────────────────┘
 */
class MainActivity : ComponentActivity() {

    companion object {
        /** Extra sent by FreeNetWidget to immediately start the VPN after launch. */
        const val EXTRA_START_VPN = "com.freenet.vpn.EXTRA_START_VPN"
    }

    private val viewModel: VpnViewModel by viewModels()

    /** Launched when the OS shows the VPN permission dialog. */
    private val vpnPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
            if (result.resultCode == Activity.RESULT_OK) {
                viewModel.startVpn(this)
            }
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // If launched from the widget, start VPN immediately (handle permission).
        if (intent?.getBooleanExtra(EXTRA_START_VPN, false) == true) {
            handleToggle()
        }
        setContent {
            FreeNetTheme {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background,
                ) {
                    FreeNetScreen(
                        viewModel  = viewModel,
                        onToggle   = ::handleToggle,
                    )
                }
            }
        }
    }

    /** Handles the big button press: requests VPN permission if needed. */
    private fun handleToggle() {
        val permIntent = viewModel.toggle(this)
        if (permIntent != null) {
            vpnPermissionLauncher.launch(permIntent)
        }
    }
}

// =============================================================================
// Compose UI
// =============================================================================

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FreeNetScreen(
    viewModel: VpnViewModel,
    onToggle:  () -> Unit,
) {
    val state              by viewModel.connectionState.collectAsState()
    val strategy           by viewModel.strategy.collectAsState()
    val stats              by viewModel.stats.collectAsState()
    val splitTunnel        by viewModel.splitTunnel.collectAsState()
    val engineStatus       by viewModel.engineStatus.collectAsState()
    // Log lines are polled in VpnViewModel and exposed via logs StateFlow.
    val logText            by viewModel.logs.collectAsState()
    val dnsBannerDismissed by viewModel.dnsBannerDismissed.collectAsState()
    val setupDismissed     by viewModel.setupDismissed.collectAsState()

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        text       = stringResource(R.string.app_name),
                        fontWeight = FontWeight.Bold,
                        fontSize   = 20.sp,
                    )
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor    = MaterialTheme.colorScheme.primary,
                    titleContentColor = MaterialTheme.colorScheme.onPrimary,
                ),
            )
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp, vertical = 32.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(28.dp),
        ) {

            // First-launch permission setup card — shown once until dismissed.
            if (!setupDismissed) {
                PermissionSetupCard(onDismiss = viewModel::dismissSetup)
            }

            // Status text.
            StatusLabel(state)

            // Engine diagnostic chip — always visible so user knows which mode is active.
            EngineStatusChip(engineStatus)

            // Big round toggle button.
            BigToggleButton(state = state, onClick = onToggle)

            // Strategy picker.
            StrategyPicker(
                selected   = strategy,
                strategies = viewModel.strategies,
                onSelect   = viewModel::setStrategy,
                enabled    = state == VpnViewModel.ConnectionState.DISCONNECTED,
            )

            // Stats card (visible only when connected).
            if (state == VpnViewModel.ConnectionState.CONNECTED && stats.isNotEmpty()) {
                StatsCard(statsJson = stats)
            }

            // DNS setup card — shown once when VPN is active until dismissed.
            if (state == VpnViewModel.ConnectionState.CONNECTED && !dnsBannerDismissed) {
                DnsSetupCard(onDismiss = viewModel::dismissDnsBanner)
            }

            // Log card (visible only when connected).
            if (state == VpnViewModel.ConnectionState.CONNECTED && logText.isNotEmpty()) {
                LogCard(text = logText)
            }

            // Diagnostics card — collapsible, shows error badge, copy + share.
            if (state == VpnViewModel.ConnectionState.CONNECTED) {
                DiagnosticsCard(
                    logText      = logText,
                    engineStatus = engineStatus,
                    onReport     = viewModel::buildReport,
                )
            }

            // Per-app split-tunnel card (always shown).
            SplitTunnelCard(
                config   = splitTunnel,
                onMode   = viewModel::setSplitTunnelMode,
                onToggle = viewModel::toggleSplitTunnelApp,
            )
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Status label
// ─────────────────────────────────────────────────────────────────────────────

@Composable
fun StatusLabel(state: VpnViewModel.ConnectionState) {
    val (text, color) = when (state) {
        VpnViewModel.ConnectionState.CONNECTED    ->
            Pair(stringResource(R.string.status_connected),    Color(0xFF2E7D32))
        VpnViewModel.ConnectionState.CONNECTING   ->
            Pair(stringResource(R.string.status_connecting),   Color(0xFFF57F17))
        VpnViewModel.ConnectionState.DISCONNECTED ->
            Pair(stringResource(R.string.status_disconnected), Color(0xFFB71C1C))
    }
    Text(
        text       = text,
        fontSize   = 18.sp,
        fontWeight = FontWeight.SemiBold,
        color      = color,
    )
}

// ─────────────────────────────────────────────────────────────────────────────
// Big round ON / OFF button
// ─────────────────────────────────────────────────────────────────────────────

@Composable
fun BigToggleButton(
    state:   VpnViewModel.ConnectionState,
    onClick: () -> Unit,
) {
    val isConnected = state == VpnViewModel.ConnectionState.CONNECTED
    val isConnecting = state == VpnViewModel.ConnectionState.CONNECTING

    val bgColor = when (state) {
        VpnViewModel.ConnectionState.CONNECTED    -> Color(0xFF1565C0) // deep blue
        VpnViewModel.ConnectionState.CONNECTING   -> Color(0xFFEF6C00) // orange
        VpnViewModel.ConnectionState.DISCONNECTED -> Color(0xFFE53935) // red
    }

    val label = if (isConnected)
        stringResource(R.string.btn_disconnect)
    else
        stringResource(R.string.btn_connect)

    Button(
        onClick  = onClick,
        enabled  = !isConnecting,
        shape    = CircleShape,
        colors   = ButtonDefaults.buttonColors(containerColor = bgColor),
        modifier = Modifier.size(180.dp),
        elevation = ButtonDefaults.buttonElevation(defaultElevation = 8.dp),
    ) {
        if (isConnecting) {
            CircularProgressIndicator(
                color    = Color.White,
                modifier = Modifier.size(36.dp),
                strokeWidth = 3.dp,
            )
        } else {
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) {
                // Power icon (unicode shield/power symbol).
                Text(
                    text     = if (isConnected) "⏹" else "▶",
                    fontSize = 36.sp,
                    color    = Color.White,
                )
                Spacer(Modifier.height(6.dp))
                Text(
                    text       = label,
                    fontSize   = 15.sp,
                    fontWeight = FontWeight.Bold,
                    color      = Color.White,
                    textAlign  = TextAlign.Center,
                )
            }
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Strategy picker
// ─────────────────────────────────────────────────────────────────────────────

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun StrategyPicker(
    selected:   String,
    strategies: List<String>,
    onSelect:   (String) -> Unit,
    enabled:    Boolean,
) {
    var expanded by remember { mutableStateOf(false) }

    Column(modifier = Modifier.fillMaxWidth()) {
        Text(
            text     = stringResource(R.string.settings_strategy),
            fontSize = 14.sp,
            color    = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(6.dp))
        ExposedDropdownMenuBox(
            expanded  = expanded,
            onExpandedChange = { if (enabled) expanded = !expanded },
        ) {
            OutlinedTextField(
                value         = selected,
                onValueChange = {},
                readOnly      = true,
                enabled       = enabled,
                modifier      = Modifier
                    .menuAnchor()
                    .fillMaxWidth(),
                trailingIcon  = { ExposedDropdownMenuDefaults.TrailingIcon(expanded) },
                label         = { Text(stringResource(R.string.settings_strategy)) },
            )
            ExposedDropdownMenu(
                expanded  = expanded,
                onDismissRequest = { expanded = false },
            ) {
                strategies.forEach { s ->
                    DropdownMenuItem(
                        text    = { Text(s) },
                        onClick = {
                            onSelect(s)
                            expanded = false
                        },
                    )
                }
            }
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Stats card
// ─────────────────────────────────────────────────────────────────────────────

@Composable
fun StatsCard(statsJson: String) {
    // Parse the JSON manually — no external JSON library required.
    fun extract(json: String, key: String): String {
        val pattern = Regex(""""$key"\s*:\s*(\d+)""")
        return pattern.find(json)?.groupValues?.get(1) ?: "0"
    }

    val active      = extract(statsJson, "active")
    val total       = extract(statsJson, "total")
    val bytesIn     = extract(statsJson, "bytes_in").toLongOrNull() ?: 0L
    val bytesOut    = extract(statsJson, "bytes_out").toLongOrNull() ?: 0L
    val bypassed    = extract(statsJson, "bypassed")
    val passthrough = extract(statsJson, "passthrough")

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape    = RoundedCornerShape(12.dp),
    ) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text("Статистика", fontWeight = FontWeight.SemiBold, fontSize = 14.sp)
            HorizontalDivider()
            StatRow("Активных соединений", active)
            StatRow("Всего соединений",    total)
            StatRow("С обходом DPI",       bypassed)
            StatRow("Прямых",              passthrough)
            StatRow("Входящих",            formatBytes(bytesIn))
            StatRow("Исходящих",           formatBytes(bytesOut))
        }
    }
}

@Composable
fun StatRow(label: String, value: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(label, fontSize = 13.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, fontSize = 13.sp, fontWeight = FontWeight.Medium)
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Log card
// ─────────────────────────────────────────────────────────────────────────────

@Composable
fun LogCard(text: String) {
    val scrollState = rememberScrollState()
    // Auto-scroll to bottom when new lines arrive.
    LaunchedEffect(text) { scrollState.animateScrollTo(scrollState.maxValue) }
    val context = LocalContext.current

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape    = RoundedCornerShape(12.dp),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            // Header row: title + copy button.
            Row(
                modifier              = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment     = Alignment.CenterVertically,
            ) {
                Text("Лог", fontWeight = FontWeight.SemiBold, fontSize = 14.sp)
                // Copy-to-clipboard button — lets the user share log lines
                // for debugging without needing to screenshot individual lines.
                TextButton(
                    onClick = {
                        val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE)
                                as ClipboardManager
                        clipboard.setPrimaryClip(ClipData.newPlainText("FreeNet logs", text))
                    },
                    contentPadding = PaddingValues(horizontal = 8.dp, vertical = 2.dp),
                ) {
                    Text("Копировать", fontSize = 12.sp)
                }
            }
            HorizontalDivider(modifier = Modifier.padding(vertical = 6.dp))
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 80.dp, max = 200.dp)
                    .background(
                        color = MaterialTheme.colorScheme.surfaceVariant,
                        shape = RoundedCornerShape(8.dp),
                    )
                    .padding(8.dp)
                    .verticalScroll(scrollState),
            ) {
                Text(
                    text       = text,
                    fontSize   = 11.sp,
                    fontFamily = FontFamily.Monospace,
                    color      = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Per-app split tunnel card
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Collapsible card that lets the user choose which apps bypass FreeNet.
 *
 * Three modes:
 *  - **disabled** — all apps (default).
 *  - **allowlist** — only ticked apps go through VPN.
 *  - **blocklist** — all apps except ticked ones go through VPN.
 *
 * The app list is loaded lazily from PackageManager on a background thread.
 * Changes take effect on the next VPN start.
 */
@Composable
fun SplitTunnelCard(
    config:   SplitTunnelConfig,
    onMode:   (String) -> Unit,
    onToggle: (String) -> Unit,
) {
    val context = LocalContext.current
    var expanded by remember { mutableStateOf(false) }
    var search   by remember { mutableStateOf("") }

    // Installed user apps — loaded once and cached.
    var installedApps by remember { mutableStateOf<List<AppEntry>>(emptyList()) }
    LaunchedEffect(Unit) {
        installedApps = withContext(Dispatchers.IO) {
            val pm = context.packageManager
            pm.getInstalledApplications(0)
                .filter { (it.flags and ApplicationInfo.FLAG_SYSTEM) == 0 }
                .map { AppEntry(it.packageName, pm.getApplicationLabel(it).toString()) }
                .sortedBy { it.label }
        }
    }

    val modes = listOf(
        SplitTunnelConfig.MODE_DISABLED  to stringResource(R.string.split_tunnel_mode_disabled),
        SplitTunnelConfig.MODE_ALLOWLIST to stringResource(R.string.split_tunnel_mode_allowlist),
        SplitTunnelConfig.MODE_BLOCKLIST to stringResource(R.string.split_tunnel_mode_blocklist),
    )

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape    = RoundedCornerShape(12.dp),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            // Header row — tapping expands/collapses the card.
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { expanded = !expanded },
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text       = stringResource(R.string.split_tunnel_title),
                    fontWeight = FontWeight.SemiBold,
                    fontSize   = 14.sp,
                )
                Text(
                    text     = if (expanded) "▲" else "▼",
                    fontSize = 12.sp,
                    color    = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            if (expanded) {
                Spacer(Modifier.height(12.dp))
                HorizontalDivider()
                Spacer(Modifier.height(8.dp))

                // Mode radio buttons.
                modes.forEach { (mode, label) ->
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { onMode(mode) }
                            .padding(vertical = 4.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        RadioButton(
                            selected = config.mode == mode,
                            onClick  = { onMode(mode) },
                        )
                        Spacer(Modifier.width(8.dp))
                        Text(label, fontSize = 13.sp)
                    }
                }

                // App list (hidden when mode is disabled).
                if (config.mode != SplitTunnelConfig.MODE_DISABLED) {
                    Spacer(Modifier.height(8.dp))
                    HorizontalDivider()
                    Spacer(Modifier.height(8.dp))

                    // Search field.
                    OutlinedTextField(
                        value         = search,
                        onValueChange = { search = it },
                        placeholder   = { Text("Поиск приложения…", fontSize = 13.sp) },
                        singleLine    = true,
                        modifier      = Modifier.fillMaxWidth(),
                    )
                    Spacer(Modifier.height(8.dp))

                    val filtered = installedApps.filter {
                        search.isEmpty() ||
                        it.label.contains(search, ignoreCase = true) ||
                        it.packageName.contains(search, ignoreCase = true)
                    }

                    if (filtered.isEmpty()) {
                        Text(
                            text     = if (installedApps.isEmpty()) "Загрузка…" else "Нет совпадений",
                            fontSize = 13.sp,
                            color    = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    } else {
                        // Show up to 20 rows in a fixed-height box.
                        val visible = filtered.take(20)
                        LazyColumn(modifier = Modifier.heightIn(max = 300.dp)) {
                            items(visible) { app ->
                                val checked = app.packageName in config.apps
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clickable { onToggle(app.packageName) }
                                        .padding(vertical = 2.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    Checkbox(
                                        checked  = checked,
                                        onCheckedChange = { onToggle(app.packageName) },
                                    )
                                    Column(modifier = Modifier.weight(1f)) {
                                        Text(app.label, fontSize = 13.sp, fontWeight = FontWeight.Medium)
                                        Text(
                                            app.packageName,
                                            fontSize = 11.sp,
                                            color    = MaterialTheme.colorScheme.onSurfaceVariant,
                                        )
                                    }
                                }
                            }
                        }
                        if (filtered.size > 20) {
                            Text(
                                text     = "…и ещё ${filtered.size - 20} приложений. Уточните поиск.",
                                fontSize = 12.sp,
                                color    = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.padding(top = 4.dp),
                            )
                        }
                    }

                    Spacer(Modifier.height(6.dp))
                    Text(
                        text     = "Изменения применяются при следующем запуске VPN.",
                        fontSize = 11.sp,
                        color    = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

/** Minimal data class holding one installed app entry. */
private data class AppEntry(val packageName: String, val label: String)

// ─────────────────────────────────────────────────────────────────────────────
// Diagnostics card
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Collapsible card that summarises engine health and lets the user copy or
 * share a full plain-text diagnostic report.
 *
 * - Collapsed: shows "📊 Диагностика" header + red badge if errors detected.
 * - Expanded:  engine-status line, error count, last-20-lines log preview,
 *              "Скопировать" and "Поделиться" buttons.
 *
 * [onReport] is called synchronously on click — it builds the report string
 * from ViewModel state (pure string work, no I/O).
 */
@Composable
fun DiagnosticsCard(
    logText:      String,
    engineStatus: String,
    onReport:     () -> String,
) {
    val context  = LocalContext.current
    var expanded by remember { mutableStateOf(false) }

    // Count error-level lines so we can show the badge even when collapsed.
    val errorCount = remember(logText) {
        logText.lines().count { line ->
            line.lowercase().let { "error" in it || "fatal" in it || "fail" in it }
        }
    }

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape    = RoundedCornerShape(12.dp),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {

            // ── Header row ─────────────────────────────────────────────────
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { expanded = !expanded },
                verticalAlignment     = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Row(
                    verticalAlignment     = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text("📊 Диагностика", fontWeight = FontWeight.SemiBold, fontSize = 14.sp)
                    if (errorCount > 0) {
                        Surface(
                            shape    = RoundedCornerShape(8.dp),
                            color    = Color(0xFFFFCDD2),
                        ) {
                            Text(
                                text     = "$errorCount ошибок",
                                fontSize = 11.sp,
                                color    = Color(0xFFB71C1C),
                                modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
                            )
                        }
                    }
                }
                Text(
                    text  = if (expanded) "▲" else "▼",
                    fontSize = 12.sp,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            // ── Expanded content ───────────────────────────────────────────
            if (expanded) {
                Spacer(Modifier.height(12.dp))
                HorizontalDivider()
                Spacer(Modifier.height(10.dp))

                // Engine status line.
                Text(
                    text     = engineStatus,
                    fontSize = 11.sp,
                    color    = MaterialTheme.colorScheme.onSurfaceVariant,
                )

                // Error summary.
                if (errorCount > 0) {
                    Spacer(Modifier.height(6.dp))
                    Text(
                        text       = "⚠ Ошибок в логе: $errorCount — скопируйте отчёт и отправьте в поддержку",
                        fontSize   = 12.sp,
                        color      = Color(0xFFB71C1C),
                        fontWeight = FontWeight.Medium,
                    )
                }

                // Action buttons.
                Spacer(Modifier.height(12.dp))
                Row(
                    modifier              = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    OutlinedButton(
                        onClick  = {
                            val report    = onReport()
                            val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE)
                                    as android.content.ClipboardManager
                            clipboard.setPrimaryClip(
                                android.content.ClipData.newPlainText("FreeNet Diagnostics", report)
                            )
                        },
                        modifier = Modifier.weight(1f),
                    ) {
                        Text("📋 Скопировать", fontSize = 12.sp)
                    }
                    OutlinedButton(
                        onClick  = {
                            val report = onReport()
                            val intent = android.content.Intent(android.content.Intent.ACTION_SEND).apply {
                                type = "text/plain"
                                putExtra(android.content.Intent.EXTRA_SUBJECT, "FreeNet — Диагностика")
                                putExtra(android.content.Intent.EXTRA_TEXT, report)
                            }
                            context.startActivity(
                                android.content.Intent.createChooser(intent, "Поделиться отчётом")
                            )
                        },
                        modifier = Modifier.weight(1f),
                    ) {
                        Text("📤 Поделиться", fontSize = 12.sp)
                    }
                }

                // Log preview — last 20 lines.
                Spacer(Modifier.height(10.dp))
                HorizontalDivider()
                Spacer(Modifier.height(6.dp))
                Text("Последние записи:", fontSize = 12.sp, fontWeight = FontWeight.Medium)
                Spacer(Modifier.height(4.dp))

                val preview = remember(logText) {
                    logText.lines().takeLast(20).joinToString("\n")
                }
                val previewScroll = rememberScrollState()
                LaunchedEffect(preview) { previewScroll.animateScrollTo(previewScroll.maxValue) }

                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(max = 180.dp)
                        .background(
                            color = MaterialTheme.colorScheme.surfaceVariant,
                            shape = RoundedCornerShape(8.dp),
                        )
                        .padding(8.dp)
                        .verticalScroll(previewScroll),
                ) {
                    Text(
                        text       = preview,
                        fontSize   = 10.sp,
                        fontFamily = FontFamily.Monospace,
                        color      = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// First-launch permission setup card
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Card shown once on first launch to explain and request the permissions
 * FreeNet needs to work correctly:
 *
 *  1. POST_NOTIFICATIONS (Android 13+) — to show the VPN-running notification
 *     in the status bar.  On older Android this is granted automatically.
 *
 *  2. VPN — explained here; the system dialog appears automatically when the
 *     user taps the big Connect button (no extra step needed).
 *
 * Tapping "Готово" persists the dismissal in SharedPreferences so the card
 * is never shown again.  When VPN is later disconnected, all settings return
 * to their original state automatically — FreeNet makes no permanent changes.
 */
@Composable
fun PermissionSetupCard(onDismiss: () -> Unit) {
    val context = LocalContext.current

    // Track notification permission state so the button updates after granting.
    var notifGranted by remember {
        mutableStateOf(
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                context.checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) ==
                    PackageManager.PERMISSION_GRANTED
            } else {
                true  // Android < 13: granted automatically at install
            }
        )
    }

    // Launcher for the POST_NOTIFICATIONS runtime permission dialog.
    val notifLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted ->
        notifGranted = granted
    }

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape    = RoundedCornerShape(12.dp),
        colors   = CardDefaults.cardColors(containerColor = Color(0xFFE3F2FD)),
    ) {
        Column(
            modifier            = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            // ── Header ───────────────────────────────────────────────────────
            Text(
                text       = "🔐 Разрешения FreeNet",
                fontWeight = FontWeight.SemiBold,
                fontSize   = 15.sp,
                color      = Color(0xFF0D47A1),
            )
            Text(
                text     = "При отключении VPN все настройки вернутся в исходное состояние автоматически.",
                fontSize = 12.sp,
                color    = Color(0xFF1565C0),
            )
            HorizontalDivider(color = Color(0xFFBBDEFB))

            // ── Permission 1: Notifications ──────────────────────────────────
            PermissionRow(
                icon        = if (notifGranted) "✅" else "🔔",
                title       = "Уведомления",
                description = "Статус VPN в строке уведомлений (\"FreeNet работает\").",
                granted     = notifGranted,
                buttonLabel = "Разрешить уведомления",
                onRequest   = {
                    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                        notifLauncher.launch(android.Manifest.permission.POST_NOTIFICATIONS)
                    }
                },
            )

            HorizontalDivider(color = Color(0xFFBBDEFB))

            // ── Permission 2: VPN ─────────────────────────────────────────────
            // VPN permission cannot be requested independently — it is shown
            // automatically by the OS when the user taps the Connect button.
            PermissionRow(
                icon        = "🌐",
                title       = "VPN",
                description = "Запрашивается автоматически при первом нажатии кнопки Включить.",
                granted     = true,   // shown as info, not requestable
                buttonLabel = "",
                onRequest   = {},
            )

            HorizontalDivider(color = Color(0xFFBBDEFB))

            // ── Done button ───────────────────────────────────────────────────
            Button(
                onClick  = onDismiss,
                modifier = Modifier.fillMaxWidth(),
                colors   = ButtonDefaults.buttonColors(
                    containerColor = Color(0xFF1565C0),
                ),
            ) {
                Text(
                    text     = if (notifGranted) "Готово ✓" else "Продолжить без уведомлений",
                    fontSize = 14.sp,
                )
            }
        }
    }
}

/**
 * Single permission row inside [PermissionSetupCard].
 *
 * Shows icon, title, description.  If [granted] is false and [buttonLabel]
 * is non-empty, shows an outlined request button.
 */
@Composable
private fun PermissionRow(
    icon:        String,
    title:       String,
    description: String,
    granted:     Boolean,
    buttonLabel: String,
    onRequest:   () -> Unit,
) {
    Row(
        modifier          = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(text = icon, fontSize = 22.sp)
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text       = title,
                fontSize   = 13.sp,
                fontWeight = FontWeight.SemiBold,
                color      = Color(0xFF0D47A1),
            )
            Text(
                text     = description,
                fontSize = 11.sp,
                color    = Color(0xFF1565C0),
            )
        }
        if (!granted && buttonLabel.isNotEmpty()) {
            OutlinedButton(
                onClick = onRequest,
                colors  = ButtonDefaults.outlinedButtonColors(
                    contentColor = Color(0xFF1565C0),
                ),
                contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
            ) {
                Text(text = buttonLabel, fontSize = 11.sp)
            }
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// DNS setup card — per-browser one-tap shortcuts
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Metadata for one browser entry in the DNS setup card.
 *
 * [settingsHint] is a human-readable navigation path shown below the browser
 * button so the user knows exactly what to tap after the browser opens.
 * Internal settings URIs (chrome://settings/…, about:config, etc.) are NOT
 * used because Android blocks external apps from opening browser-internal pages
 * for security reasons — they either open the browser home page, open multiple
 * blank tabs, or do nothing at all.
 */
private data class BrowserDnsInfo(
    val packageName: String,
    val displayName: String,
    val settingsHint: String,   // step-by-step path shown as text below the button
)

/**
 * Known browsers ordered by Russian market share.
 * All entries show the exact in-browser navigation path the user must follow
 * after the button opens the browser.
 */
private val BROWSER_DNS_LIST = listOf(
    // ── Google Chrome family ──────────────────────────────────────────────────
    BrowserDnsInfo("com.android.chrome",
        "Chrome",
        "⋮ → Настройки → Конфиденциальность → Безопасность → Использовать защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.chrome.beta",
        "Chrome Beta",
        "⋮ → Настройки → Конфиденциальность → Безопасность → Использовать защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.chrome.dev",
        "Chrome Dev",
        "⋮ → Настройки → Конфиденциальность → Безопасность → Использовать защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.chrome.canary",
        "Chrome Canary",
        "⋮ → Настройки → Конфиденциальность → Безопасность → Использовать защищённый DNS → ВЫКЛ"),
    // ── Yandex Browser ───────────────────────────────────────────────────────
    BrowserDnsInfo("com.yandex.browser",
        "Яндекс Браузер",
        "≡ → Настройки → Безопасность → Защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.yandex.browser.beta",
        "Яндекс Бета",
        "≡ → Настройки → Безопасность → Защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.yandex.browser.lite",
        "Яндекс Лайт",
        "≡ → Настройки → Безопасность → Защищённый DNS → ВЫКЛ"),
    // ── Mozilla Firefox ──────────────────────────────────────────────────────
    BrowserDnsInfo("org.mozilla.firefox",
        "Firefox",
        "≡ → Настройки → Конфиденциальность → DNS через HTTPS → ВЫКЛ"),
    BrowserDnsInfo("org.mozilla.firefox_beta",
        "Firefox Beta",
        "≡ → Настройки → Конфиденциальность → DNS через HTTPS → ВЫКЛ"),
    BrowserDnsInfo("org.mozilla.fenix",
        "Firefox Nightly",
        "≡ → Настройки → Конфиденциальность → DNS через HTTPS → ВЫКЛ"),
    // ── Brave ────────────────────────────────────────────────────────────────
    BrowserDnsInfo("com.brave.browser",
        "Brave",
        "⋮ → Настройки → Конфиденциальность → Использовать защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.brave.browser_beta",
        "Brave Beta",
        "⋮ → Настройки → Конфиденциальность → Использовать защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.brave.browser_nightly",
        "Brave Nightly",
        "⋮ → Настройки → Конфиденциальность → Использовать защищённый DNS → ВЫКЛ"),
    // ── Microsoft Edge ───────────────────────────────────────────────────────
    BrowserDnsInfo("com.microsoft.emmx",
        "Edge",
        "… → Настройки → Конфиденциальность → Безопасность → Использовать защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.microsoft.emmx.beta",
        "Edge Beta",
        "… → Настройки → Конфиденциальность → Безопасность → Использовать защищённый DNS → ВЫКЛ"),
    // ── Opera ────────────────────────────────────────────────────────────────
    BrowserDnsInfo("com.opera.browser",
        "Opera",
        "O → Настройки → Конфиденциальность → DNS-over-HTTPS → ВЫКЛ"),
    BrowserDnsInfo("com.opera.browser.beta",
        "Opera Beta",
        "O → Настройки → Конфиденциальность → DNS-over-HTTPS → ВЫКЛ"),
    BrowserDnsInfo("com.opera.gx_mobile",
        "Opera GX",
        "O → Настройки → Конфиденциальность → DNS-over-HTTPS → ВЫКЛ"),
    BrowserDnsInfo("com.opera.mini.native",
        "Opera Mini",
        "O → Настройки → Дополнительно → DNS-over-HTTPS → ВЫКЛ"),
    // ── Samsung Internet ─────────────────────────────────────────────────────
    BrowserDnsInfo("com.sec.android.app.sbrowser",
        "Samsung Internet",
        "≡ → Настройки → Конфиденциальность → DNS over HTTPS → ВЫКЛ"),
    // ── Kiwi ─────────────────────────────────────────────────────────────────
    BrowserDnsInfo("com.kiwibrowser.browser",
        "Kiwi",
        "⋮ → Настройки → Конфиденциальность → Безопасность → Использовать защищённый DNS → ВЫКЛ"),
    // ── DuckDuckGo ───────────────────────────────────────────────────────────
    BrowserDnsInfo("com.duckduckgo.mobile.android",
        "DuckDuckGo",
        "≡ → Настройки → Конфиденциальность → Private DNS → ВЫКЛ"),
    // ── OEM browsers ─────────────────────────────────────────────────────────
    BrowserDnsInfo("com.mi.globalbrowser",
        "Mi Browser",
        "≡ → Настройки → Безопасность → Защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.huawei.browser",
        "Huawei Browser",
        "≡ → Настройки → Безопасность → DNS-over-HTTPS → ВЫКЛ"),
    BrowserDnsInfo("com.vivo.browser",
        "Vivo Browser",
        "≡ → Настройки → Безопасность → Защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.heytap.browser",
        "OPPO Browser",
        "≡ → Настройки → Безопасность → DNS-over-HTTPS → ВЫКЛ"),
    // ── Other ────────────────────────────────────────────────────────────────
    BrowserDnsInfo("com.naver.whale.android",
        "Whale",
        "≡ → Настройки → Конфиденциальность → Защищённый DNS → ВЫКЛ"),
    BrowserDnsInfo("com.UCMobile.intl",
        "UC Browser",
        "≡ → Настройки → Безопасность → DNS → ВЫКЛ"),
    BrowserDnsInfo("mark.via.gp",
        "Via",
        "≡ → Настройки → Прокси → DNS-over-HTTPS → ВЫКЛ"),
)

/**
 * DNS setup card — shown once after VPN connects, dismissed permanently on tap.
 *
 * Step 1: Opens Android Private DNS settings via a fallback chain of intent
 * actions (OEMs use different action strings; we try them in order and fall
 * back to a manual path Toast if none work).
 *
 * Step 2: For each installed browser — one button that opens the browser's
 * home screen, plus a persistent hint line showing the exact in-browser
 * navigation path to the Secure DNS toggle.  Internal settings URIs such as
 * chrome://settings/security are intentionally NOT used here because Android
 * blocks external apps from opening browser-internal pages; they cause blank
 * tabs, multiple windows, or silence.
 */
@Composable
fun DnsSetupCard(onDismiss: () -> Unit) {
    val context = LocalContext.current

    // Filter to only installed browsers — checked once at composition.
    val installedBrowsers = remember {
        BROWSER_DNS_LIST.filter { info ->
            try { context.packageManager.getPackageInfo(info.packageName, 0); true }
            catch (_: Exception) { false }
        }
    }

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape    = RoundedCornerShape(12.dp),
        colors   = CardDefaults.cardColors(containerColor = Color(0xFFFFF8E1)),
    ) {
        Column(
            modifier            = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            // ── Header ───────────────────────────────────────────────────────
            Text(
                text       = "🌐 Один тап — браузер работает",
                fontWeight = FontWeight.SemiBold,
                fontSize   = 14.sp,
                color      = Color(0xFF4E342E),
            )
            HorizontalDivider(color = Color(0xFFFFECB3))

            // ── Step 1: Android Private DNS ──────────────────────────────────
            Text(
                text       = "Шаг 1 — Частный DNS Android",
                fontSize   = 13.sp,
                fontWeight = FontWeight.Medium,
                color      = Color(0xFF5D4037),
            )
            // Try a chain of action strings — OEMs (Samsung, Xiaomi, Huawei)
            // may not register android.settings.PRIVATE_DNS_SETTINGS.
            // We fall back through wireless settings to the root settings screen,
            // and if all fail, show a Toast with the manual navigation path.
            Button(
                onClick  = {
                    val fallbackActions = listOf(
                        "android.settings.PRIVATE_DNS_SETTINGS",   // stock Android 9+
                        "android.settings.WIRELESS_SETTINGS",       // all Androids
                        "android.settings.SETTINGS",                // last resort
                    )
                    val opened = fallbackActions.any { action ->
                        try {
                            context.startActivity(
                                Intent(action).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                            )
                            true
                        } catch (_: Exception) { false }
                    }
                    if (!opened) {
                        android.widget.Toast.makeText(
                            context,
                            "Настройки → Подключения → Другие сети → Частный DNS → Выключить",
                            android.widget.Toast.LENGTH_LONG,
                        ).show()
                    }
                },
                modifier = Modifier.fillMaxWidth(),
                colors   = ButtonDefaults.buttonColors(containerColor = Color(0xFF6D4C41)),
            ) {
                Text("Открыть настройки DNS Android  →", fontSize = 13.sp)
            }
            Text(
                text     = "Найти «Частный DNS» и выбрать «Выключить».",
                fontSize = 11.sp,
                color    = Color(0xFF795548),
            )

            // ── Step 2: Browser Secure DNS ───────────────────────────────────
            HorizontalDivider(color = Color(0xFFFFECB3))
            Text(
                text       = "Шаг 2 — Защищённый DNS в браузерах",
                fontSize   = 13.sp,
                fontWeight = FontWeight.Medium,
                color      = Color(0xFF5D4037),
            )
            Text(
                text     = "Нажмите кнопку → откроется браузер → следуйте подсказке.",
                fontSize = 11.sp,
                color    = Color(0xFF795548),
            )

            if (installedBrowsers.isEmpty()) {
                Text(
                    text     = "Ни один из известных браузеров не установлен.",
                    fontSize = 12.sp,
                    color    = Color(0xFF795548),
                )
            } else {
                // Each browser: one launch button + a persistent navigation hint below it.
                // Internal settings URIs (chrome://settings/…, about:config, etc.) are
                // blocked by Android from external apps, so we open the browser normally
                // and show the user the exact tap path to reach the DNS toggle.
                installedBrowsers.forEach { info ->
                    Column(
                        modifier            = Modifier.fillMaxWidth(),
                        verticalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        OutlinedButton(
                            onClick  = {
                                val intent = context.packageManager
                                    .getLaunchIntentForPackage(info.packageName)
                                    ?.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                                if (intent != null) context.startActivity(intent)
                            },
                            modifier = Modifier.fillMaxWidth(),
                            colors   = ButtonDefaults.outlinedButtonColors(
                                contentColor = Color(0xFF4E342E),
                            ),
                        ) {
                            Text(
                                text     = "Открыть ${info.displayName}  →",
                                fontSize = 13.sp,
                            )
                        }
                        // Navigation hint — always visible, user reads it before/after tapping.
                        Text(
                            text     = info.settingsHint,
                            fontSize = 11.sp,
                            color    = Color(0xFF6D4C41),
                            modifier = Modifier.padding(start = 6.dp),
                        )
                    }
                }
            }

            // ── Dismiss ──────────────────────────────────────────────────────
            HorizontalDivider(color = Color(0xFFFFECB3))
            TextButton(
                onClick  = onDismiss,
                modifier = Modifier.align(Alignment.End),
            ) {
                Text("Понятно, скрыть", fontSize = 12.sp, color = Color(0xFF795548))
            }
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Theme
// ─────────────────────────────────────────────────────────────────────────────

@Composable
fun FreeNetTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = lightColorScheme(
            primary         = Color(0xFF1565C0),
            onPrimary       = Color.White,
            secondary       = Color(0xFF1976D2),
            background      = Color(0xFFF5F5F5),
            surface         = Color.White,
            surfaceVariant  = Color(0xFFECEFF1),
            onSurfaceVariant = Color(0xFF607D8B),
        ),
        content = content,
    )
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// Log and stats polling is done in VpnViewModel using FreenetVpnService.instance.
// See VpnViewModel.startPolling() / stopPolling().

// ─────────────────────────────────────────────────────────────────────────────
// Engine status chip
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Small informational chip showing whether the Go bypass engine is active.
 * - Green tint  → Go AAR loaded and active (full bypass + DoH DNS).
 * - Amber tint  → Kotlin fallback (TCP only, no DNS protection).
 * - Grey tint   → Service not yet started.
 *
 * This is primarily a diagnostic aid so the user knows whether the Go engine
 * was successfully loaded from the compiled AAR.
 */
@Composable
fun EngineStatusChip(status: String) {
    val isKotlinFallback = status.contains("fallback", ignoreCase = true) ||
                           status.contains("Kotlin",   ignoreCase = true)
    val isError          = status.contains("Ошибка",  ignoreCase = true)
    val bgColor = when {
        isError          -> Color(0xFFFFCDD2) // red tint
        isKotlinFallback -> Color(0xFFFFF9C4) // amber tint
        else             -> Color(0xFFE8F5E9) // green tint
    }
    val textColor = when {
        isError          -> Color(0xFFB71C1C)
        isKotlinFallback -> Color(0xFFE65100)
        else             -> Color(0xFF1B5E20)
    }
    Surface(
        shape  = RoundedCornerShape(20.dp),
        color  = bgColor,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 4.dp),
    ) {
        Text(
            text       = status,
            fontSize   = 11.sp,
            color      = textColor,
            modifier   = Modifier.padding(horizontal = 12.dp, vertical = 5.dp),
            textAlign  = TextAlign.Center,
        )
    }
}

private fun formatBytes(bytes: Long): String = when {
    bytes < 1_024            -> "$bytes B"
    bytes < 1_048_576        -> "%.1f КБ".format(bytes / 1_024.0)
    bytes < 1_073_741_824    -> "%.1f МБ".format(bytes / 1_048_576.0)
    else                     -> "%.2f ГБ".format(bytes / 1_073_741_824.0)
}
