package com.freenet.vpn

import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import androidx.activity.ComponentActivity
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

            // DNS setup reminder — shown once when VPN is active until dismissed.
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
// DNS setup card
// ─────────────────────────────────────────────────────────────────────────────

/**
 * One-time reminder card that guides the user to disable Android's Private DNS
 * and Chrome's Secure DNS, which would otherwise break browser traffic by
 * sending encrypted DNS queries on port 853 — a port that is typically blocked
 * by Russian ISPs and now RST'd by FreeNet's TUN layer to force fallback.
 *
 * The card is shown while the VPN is active and hidden permanently once the
 * user taps "Понятно".  The dismissed state persists in SharedPreferences so
 * it survives app restarts.
 *
 * On Android 10+ (API 29) the "Open DNS Settings" button navigates directly to
 * the system Private DNS screen.  On older versions a textual instruction is
 * shown instead (Private DNS was introduced in Android 9 / API 28 but the
 * dedicated Settings action only exists in API 29+).
 */
@Composable
fun DnsSetupCard(onDismiss: () -> Unit) {
    val context = LocalContext.current
    // Detect whether we can open the Private DNS settings screen directly.
    val canOpenDnsSettings = Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape    = RoundedCornerShape(12.dp),
        colors   = CardDefaults.cardColors(
            containerColor = Color(0xFFFFF8E1), // warm amber tint — informational
        ),
    ) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            // Header.
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text       = "⚙️ Настройка DNS",
                    fontWeight = FontWeight.SemiBold,
                    fontSize   = 14.sp,
                    color      = Color(0xFF5D4037),
                )
            }

            HorizontalDivider(color = Color(0xFFFFECB3))

            // Explanation.
            Text(
                text = "Для работы браузера через FreeNet отключите зашифрованный DNS:\n\n" +
                       "1. Настройки → Подключения → Другие настройки → Частный DNS → Выключить\n" +
                       "2. Chrome → адрес chrome://settings/security → " +
                       "«Использовать защищённый DNS» → Выключить",
                fontSize = 12.sp,
                color    = Color(0xFF4E342E),
            )

            // Action buttons.
            Row(
                modifier              = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                if (canOpenDnsSettings) {
                    // Direct shortcut on Android 10+.
                    OutlinedButton(
                        onClick  = {
                            context.startActivity(
                                Intent(Settings.ACTION_PRIVATE_DNS_SETTINGS)
                                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                            )
                        },
                        modifier = Modifier.weight(1f),
                        colors   = ButtonDefaults.outlinedButtonColors(
                            contentColor = Color(0xFF5D4037),
                        ),
                    ) {
                        Text("Открыть настройки DNS", fontSize = 12.sp)
                    }
                }
                OutlinedButton(
                    onClick  = onDismiss,
                    modifier = Modifier.weight(1f),
                    colors   = ButtonDefaults.outlinedButtonColors(
                        contentColor = Color(0xFF5D4037),
                    ),
                ) {
                    Text("Понятно", fontSize = 12.sp)
                }
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
