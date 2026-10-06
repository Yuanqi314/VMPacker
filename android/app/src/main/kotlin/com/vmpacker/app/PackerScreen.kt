package com.vmpacker.app

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.clickable
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateList
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.vmpacker.vmpmobile.Vmpmobile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import top.yukonga.miuix.kmp.basic.Button
import top.yukonga.miuix.kmp.basic.ButtonDefaults
import top.yukonga.miuix.kmp.basic.Card
import top.yukonga.miuix.kmp.basic.Scaffold
import top.yukonga.miuix.kmp.basic.Text
import top.yukonga.miuix.kmp.basic.TextField
import top.yukonga.miuix.kmp.basic.TopAppBar
import top.yukonga.miuix.kmp.theme.MiuixTheme
import top.yukonga.miuix.kmp.theme.darkColorScheme
import top.yukonga.miuix.kmp.theme.lightColorScheme
import androidx.compose.runtime.rememberCoroutineScope
import java.io.File

private data class FuncInfo(val name: String, val address: String, val size: Long)

@Composable
fun PackerApp() {
    MiuixTheme(
        colors = if (isSystemInDarkTheme()) darkColorScheme() else lightColorScheme()
    ) {
        PackerScreen()
    }
}

@Composable
private fun PackerScreen() {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var inputFile by remember { mutableStateOf<File?>(null) }
    var inputName by remember { mutableStateOf("") }
    var arch by remember { mutableStateOf("") }
    var format by remember { mutableStateOf("") }
    val functions = remember { mutableStateListOf<FuncInfo>() }
    val selected = remember { mutableStateListOf<String>() }
    var query by remember { mutableStateOf("") }

    var strip by remember { mutableStateOf(true) }
    var token by remember { mutableStateOf(true) }
    var debug by remember { mutableStateOf(false) }

    var busy by remember { mutableStateOf(false) }
    var status by remember { mutableStateOf("请选择一个 ARM64 ELF 文件进行保护") }
    val logger = remember { ComposeLogger() }

    // Pick an input file -> copy to cache -> analyze.
    val openInput = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenDocument()
    ) { uri ->
        uri ?: return@rememberLauncherForActivityResult
        val name = displayName(context, uri, "input.elf")
        scope.launch {
            busy = true
            status = "正在分析 $name ..."
            functions.clear(); selected.clear(); query = ""; arch = ""; format = ""
            try {
                val (file, json) = withContext(Dispatchers.IO) {
                    val f = copyUriToCache(context, uri, "input.bin")
                    f to Vmpmobile.analyze(f.absolutePath)
                }
                inputFile = file
                inputName = name
                val meta = parseAnalysis(json, functions)
                arch = meta.first
                format = meta.second
                status = if (functions.isEmpty()) {
                    "未发现可保护的函数 (可能已被完全 strip)"
                } else {
                    "发现 ${functions.size} 个可保护函数，勾选后开始保护"
                }
            } catch (e: Exception) {
                status = "分析失败: ${e.message}"
            } finally {
                busy = false
            }
        }
    }

    // Pick an output location -> run the engine -> export the result.
    val createOutput = rememberLauncherForActivityResult(
        ActivityResultContracts.CreateDocument("application/octet-stream")
    ) { uri ->
        val inF = inputFile
        if (uri == null || inF == null) return@rememberLauncherForActivityResult
        scope.launch {
            busy = true
            status = "正在保护 ..."
            logger.lines.clear()
            try {
                val funcsJson = buildFuncsJson(selected)
                withContext(Dispatchers.IO) {
                    val outFile = File(context.cacheDir, "packed.bin")
                    Vmpmobile.protect(
                        inF.absolutePath, outFile.absolutePath, funcsJson,
                        strip, debug, token, logger,
                    )
                    copyFileToUri(context, outFile, uri)
                }
                status = "保护完成 ✓ 已导出到所选位置"
            } catch (e: Exception) {
                status = "保护失败: ${e.message}"
            } finally {
                busy = false
            }
        }
    }

    // Functions matching the current search query.
    val shown = if (query.isBlank()) {
        functions
    } else {
        functions.filter { it.name.contains(query, ignoreCase = true) }
    }
    val allShownSelected = shown.isNotEmpty() && shown.all { selected.contains(it.name) }
    val canProtect = !busy && selected.isNotEmpty()

    Scaffold(
        topBar = { TopAppBar(title = "VMPacker") }
    ) { padding ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
        ) {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp)
                    .padding(top = 16.dp, bottom = if (functions.isNotEmpty()) 104.dp else 16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                // Status / file summary.
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(16.dp)) {
                        Text(text = status, color = MiuixTheme.colorScheme.onSurface)
                        if (arch.isNotEmpty()) {
                            Spacer(Modifier.height(4.dp))
                            Text(
                                text = "$format · $arch · $inputName",
                                color = MiuixTheme.colorScheme.onSurface.copy(alpha = 0.6f),
                            )
                        }
                    }
                }

                Button(
                    onClick = { openInput.launch(arrayOf("*/*")) },
                    enabled = !busy,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(if (inputFile == null) "选择 ELF 文件" else "重新选择文件")
                }

                if (inputFile != null) {
                    Text(text = "选项", color = MiuixTheme.colorScheme.onSurface)
                    ToggleRow("清除符号表 (strip)", strip) { strip = it }
                    ToggleRow("Token 化入口 (3 指令跳板)", token) { token = it }
                    ToggleRow("生成 debug 对照", debug) { debug = it }
                }

                if (functions.isNotEmpty()) {
                    // Header with live count + a Select All / Deselect All action.
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            text = "选择要保护的函数 (${selected.size}/${functions.size})",
                            color = MiuixTheme.colorScheme.onSurface,
                            modifier = Modifier.weight(1f),
                        )
                        Text(
                            text = if (allShownSelected) "取消全选" else "全选",
                            color = MiuixTheme.colorScheme.primary,
                            modifier = Modifier.clickable {
                                if (allShownSelected) {
                                    shown.forEach { selected.remove(it.name) }
                                } else {
                                    shown.forEach { if (!selected.contains(it.name)) selected.add(it.name) }
                                }
                            },
                        )
                    }

                    // Search box.
                    TextField(
                        value = query,
                        onValueChange = { query = it },
                        modifier = Modifier.fillMaxWidth(),
                        label = "搜索函数名",
                        singleLine = true,
                    )

                    if (shown.isEmpty()) {
                        Text(
                            text = "没有匹配 \"$query\" 的函数",
                            color = MiuixTheme.colorScheme.onSurface.copy(alpha = 0.6f),
                        )
                    }

                    shown.forEach { fn ->
                        val isSel = selected.contains(fn.name)
                        Card(
                            modifier = Modifier.fillMaxWidth(),
                            showIndication = true,
                            onClick = {
                                if (isSel) selected.remove(fn.name) else selected.add(fn.name)
                            },
                        ) {
                            Row(
                                modifier = Modifier.fillMaxWidth().padding(14.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Column(modifier = Modifier.weight(1f)) {
                                    Text(text = fn.name, color = MiuixTheme.colorScheme.onSurface)
                                    Text(
                                        text = "${fn.address} · ${fn.size} B",
                                        color = MiuixTheme.colorScheme.onSurface.copy(alpha = 0.6f),
                                    )
                                }
                                CircleCheck(selected = isSel)
                            }
                        }
                    }
                }

                if (logger.lines.isNotEmpty()) {
                    Text(text = "引擎日志", color = MiuixTheme.colorScheme.onSurface)
                    Card(modifier = Modifier.fillMaxWidth()) {
                        Column(modifier = Modifier.padding(12.dp)) {
                            val style = TextStyle(
                                color = MiuixTheme.colorScheme.onSurface.copy(alpha = 0.85f),
                                fontFamily = FontFamily.Monospace,
                                fontSize = 12.sp,
                            )
                            logger.lines.takeLast(200).forEach { line ->
                                BasicText(text = line, style = style)
                            }
                        }
                    }
                }
            }

            // "Start Protection" stays anchored in the lower part of the screen
            // (with a margin above the navigation bar), not flush to the very bottom.
            if (functions.isNotEmpty()) {
                Button(
                    onClick = { createOutput.launch("$inputName.vmp") },
                    enabled = canProtect,
                    // Blue (primary) once a function is selected, MIUIX-style; neutral otherwise.
                    colors = if (canProtect) {
                        ButtonDefaults.buttonColorsPrimary()
                    } else {
                        ButtonDefaults.buttonColors()
                    },
                    modifier = Modifier
                        .align(Alignment.BottomCenter)
                        .fillMaxWidth()
                        .navigationBarsPadding()
                        .padding(horizontal = 16.dp, vertical = 24.dp),
                ) {
                    Text(if (busy) "处理中 ..." else "开始保护 (${selected.size})")
                }
            }
        }
    }
}

@Composable
private fun ToggleRow(label: String, value: Boolean, onToggle: (Boolean) -> Unit) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        showIndication = true,
        onClick = { onToggle(!value) },
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = label,
                color = MiuixTheme.colorScheme.onSurface,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = if (value) "开" else "关",
                color = if (value) MiuixTheme.colorScheme.primary
                else MiuixTheme.colorScheme.onSurface.copy(alpha = 0.5f),
            )
        }
    }
}

/** Circular selection indicator: a filled blue circle with a white check when selected,
 *  an empty outlined circle otherwise — matching the MIUIX reference. */
@Composable
private fun CircleCheck(selected: Boolean) {
    val primary = MiuixTheme.colorScheme.primary
    val outline = MiuixTheme.colorScheme.onSurface.copy(alpha = 0.3f)
    Canvas(modifier = Modifier.size(22.dp)) {
        val d = size.minDimension
        val r = d / 2f
        val center = Offset(size.width / 2f, size.height / 2f)
        if (selected) {
            drawCircle(color = primary, radius = r, center = center)
            val check = Path().apply {
                moveTo(d * 0.28f, d * 0.52f)
                lineTo(d * 0.43f, d * 0.67f)
                lineTo(d * 0.73f, d * 0.34f)
            }
            drawPath(
                path = check,
                color = Color.White,
                style = Stroke(
                    width = d * 0.11f,
                    cap = StrokeCap.Round,
                    join = StrokeJoin.Round,
                ),
            )
        } else {
            drawCircle(
                color = outline,
                radius = r - d * 0.06f,
                center = center,
                style = Stroke(width = d * 0.08f),
            )
        }
    }
}

// org.json is part of the Android platform — no dependency required.
private fun parseAnalysis(json: String, out: SnapshotStateList<FuncInfo>): Pair<String, String> {
    val o = org.json.JSONObject(json)
    val arr = o.optJSONArray("functions") ?: org.json.JSONArray()
    for (i in 0 until arr.length()) {
        val f = arr.getJSONObject(i)
        out.add(FuncInfo(f.optString("name"), f.optString("address"), f.optLong("size")))
    }
    return o.optString("arch", "ARM64") to o.optString("format", "ELF")
}

private fun buildFuncsJson(names: List<String>): String {
    val arr = org.json.JSONArray()
    for (n in names) {
        arr.put(org.json.JSONObject().put("name", n).put("isCustom", false))
    }
    return arr.toString()
}
