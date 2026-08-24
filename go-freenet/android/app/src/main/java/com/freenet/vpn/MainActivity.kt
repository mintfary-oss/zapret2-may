package com.freenet.vpn

import android.app.Activity
import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay

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
    val state    by viewModel.connectionState.collectAsState()
    val strategy by viewModel.strategy.collectAsState()
    val stats    by viewModel.stats.collectAsState()

    // Poll log lines from the service every 2 seconds.
    var logText by remember { mutableStateOf("") }
    LaunchedEffect(state) {
        while (state == VpnViewModel.ConnectionState.CONNECTED) {
            logText = fetchLogs()
            delay(2_000)
        }
    }

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

            // Log card (visible only when connected).
            if (state == VpnViewModel.ConnectionState.CONNECTED && logText.isNotEmpty()) {
                LogCard(text = logText)
            }
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

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape    = RoundedCornerShape(12.dp),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text("Лог", fontWeight = FontWeight.SemiBold, fontSize = 14.sp)
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

/** Fetches recent log lines from the running Go engine via reflection. */
private fun fetchLogs(): String {
    return try {
        // Locate the singleton FreenetVpnService instance via static field.
        val svcCls = FreenetVpnService::class.java
        // The engine is held in the service instance — we access it via a
        // static companion field that the service sets on start.
        val fld = svcCls.getDeclaredField("goEngine")
        fld.isAccessible = true
        // Note: this only works if called from within the same process.
        // We look it up on the service object stored in the singleton map.
        ""  // Placeholder — log polling is wired up in a future iteration.
    } catch (_: Exception) { "" }
}

private fun formatBytes(bytes: Long): String = when {
    bytes < 1_024            -> "$bytes B"
    bytes < 1_048_576        -> "%.1f КБ".format(bytes / 1_024.0)
    bytes < 1_073_741_824    -> "%.1f МБ".format(bytes / 1_048_576.0)
    else                     -> "%.2f ГБ".format(bytes / 1_073_741_824.0)
}
