import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// UTH Power bar widget and anchored popup.
// The single bar-widget entry point owns the bolt icon, the collector process,
// and the KeyboardPanel dashboard. Shell IPC summon/toggle/hide routes through
// the Panel base.
Panel {
    // Intentional close followed by reopen: restart immediately.

    id: root

    // Parsed telemetry state.
    property var sourceData: ({
        "name": "",
        "power_mode": "unknown",
        "battery_status": "",
        "total_draw_w": null,
        "total_draw_kind": "none",
        "battery_percentage": -1
    })
    property var componentsData: []
    property bool dataLoaded: false
    property bool stale: false
    // Collector lifecycle state.
    property bool expectedStop: false
    property string stopReason: "user"
    property bool errorHandled: false
    property bool restartPending: false
    property int consecutiveFailures: 0
    property bool retrying: false
    property bool permanentFailure: false
    property string faultText: ""
    property int invalidLineCount: 0
    property string stderrTail: ""
    property int lastProcessId: 0
    property double lastValidSampleTime: 0
    property int healthySampleCount: 0
    property double processStartTime: 0
    property bool processStarted: false
    readonly property bool reduceMotion: Style.reduceMotion !== undefined && Style.reduceMotion === true
    readonly property var canonicalIds: ["cpu", "gpu", "ram", "disk"]
    readonly property var knownDrawKinds: ["measured", "modeled", "mixed", "none"]
    readonly property var knownComponentDrawKinds: ["measured", "modeled", "utilization"]
    readonly property int maxRetries: 6
    readonly property var backoffDelays: [500, 1000, 2000, 4000, 8000, 16000]
    readonly property int maxLineBytes: 65536
    readonly property int maxStderrTail: 200
    readonly property int healthySampleThreshold: 10
    readonly property int healthyTimeThreshold: 15000
    readonly property string binaryPath: {
        var url = String(Qt.resolvedUrl("../bin/uth-power") || "");
        if (url.indexOf("file://") === 0)
            url = url.substring(7);

        try {
            url = decodeURIComponent(url);
        } catch (e) {
        }
        return url;
    }

    function componentById(id) {
        for (var i = 0; i < componentsData.length; i++) {
            var c = componentsData[i];
            if (c && c.id === id)
                return c;

        }
        return null;
    }

    function resetFailureBudget() {
        consecutiveFailures = 0;
        retrying = false;
        permanentFailure = false;
        faultText = "";
        healthySampleCount = 0;
    }

    function startCollector() {
        if (collector.running) {
            restartPending = true;
            return ;
        }
        if (permanentFailure)
            resetFailureBudget();

        restartPending = false;
        retrying = false;
        expectedStop = false;
        stopReason = "user";
        errorHandled = false;
        processStarted = false;
        healthySampleCount = 0;
        processStartTime = Date.now();
        stderrTail = "";
        collector.running = true;
    }

    function stopCollector(reason) {
        if (reason === undefined)
            reason = "user";

        retryTimer.stop();
        watchdog.stop();
        if (collector.running) {
            expectedStop = true;
            stopReason = reason;
            lastProcessId = Number(collector.processId) || 0;
            collector.running = false;
            killTimer.restart();
        }
    }

    function scheduleRetry() {
        if (consecutiveFailures >= maxRetries) {
            permanentFailure = true;
            retrying = false;
            restartPending = false;
            faultText = "Collector failed permanently. Click the bolt to retry.";
            return ;
        }
        retrying = true;
        restartPending = true;
        var delay = backoffDelays[Math.min(consecutiveFailures - 1, backoffDelays.length - 1)];
        retryTimer.interval = delay;
        retryTimer.start();
    }

    function markHealthy() {
        healthySampleCount++;
        var elapsed = Date.now() - processStartTime;
        if (healthySampleCount >= healthySampleThreshold || elapsed >= healthyTimeThreshold) {
            consecutiveFailures = 0;
            retrying = false;
            permanentFailure = false;
            faultText = "";
        }
    }

    function appendStderr(line) {
        var s = String(line || "").trim();
        if (!s)
            return ;

        var tail = (stderrTail ? stderrTail + " | " : "") + s;
        if (tail.length > maxStderrTail)
            tail = "…" + tail.slice(-(maxStderrTail - 1));

        stderrTail = tail;
    }

    function onCollectorExited(exitCode, exitStatus) {
        killTimer.stop();
        var expected = expectedStop;
        var reason = stopReason;
        expectedStop = false;
        stopReason = "user";
        if (expected) {
            if (reason === "user" && (root.opened || root.restartPending))
                Qt.callLater(root.startCollector);

            // Failure stops rely on the already-scheduled retry timer.
            return ;
        }
        if (errorHandled)
            return ;

        consecutiveFailures++;
        if (exitStatus === 1)
            faultText = "Collector crashed" + (stderrTail ? ": " + stderrTail : "");
        else if (exitCode === 0)
            faultText = "Collector exited unexpectedly" + (stderrTail ? ": " + stderrTail : "");
        else
            faultText = "Collector exited with code " + exitCode + (stderrTail ? ": " + stderrTail : "");
        if (root.opened)
            scheduleRetry();

        errorHandled = true;
    }

    function onCollectorError(error) {
        if (errorHandled)
            return ;

        killTimer.stop();
        errorHandled = true;
        consecutiveFailures++;
        var reason;
        if (error === 0)
            reason = "Failed to start";
        else if (error === 1)
            reason = "Crashed on launch";
        else
            reason = "Launch error " + error;
        faultText = reason + (stderrTail ? ": " + stderrTail : "");
        stopCollector("failure");
        if (root.opened)
            scheduleRetry();

    }

    function killProcess() {
        var pid = Number(collector.processId) || lastProcessId;
        if (!pid)
            return ;

        if (collector.signal !== undefined && typeof collector.signal === "function") {
            try {
                collector.signal(9);
            } catch (e) {
                killer.kill(pid);
            }
        } else {
            killer.kill(pid);
        }
    }

    function clampInt(v, lo, hi) {
        var n = Math.round(Number(v));
        if (!isFinite(n))
            return lo;

        return Math.max(lo, Math.min(hi, n));
    }

    function validateTelemetry(obj) {
        if (!obj || typeof obj !== "object" || Array.isArray(obj))
            return false;

        if (obj.schema_version !== 2)
            return false;

        var src = obj.source;
        if (!src || typeof src !== "object")
            return false;

        var name = String(src.name || "").trim();
        if (name === "")
            return false;

        var kind = String(src.total_draw_kind || "");
        if (root.knownDrawKinds.indexOf(kind) < 0)
            return false;

        if (kind === "none") {
            if (src.total_draw_w !== undefined && src.total_draw_w !== null && !isFinite(Number(src.total_draw_w)))
                return false;

        } else {
            if (!isFinite(Number(src.total_draw_w)))
                return false;

        }
        var batt = src.battery_percentage;
        if (batt === undefined || batt === null || !isFinite(Number(batt)))
            return false;

        if (batt < -1 || batt > 100)
            return false;

        if (!Array.isArray(obj.components))
            return false;

        return true;
    }

    function normalizeComponents(list) {
        var out = [];
        var seen = {
        };
        for (var i = 0; i < list.length; i++) {
            var c = list[i];
            if (!c || typeof c !== "object")
                continue;

            var id = String(c.id || "");
            if (root.canonicalIds.indexOf(id) < 0)
                continue;

            if (seen[id])
                continue;

            seen[id] = true;
            var drawKind = String(c.draw_kind || "measured");
            if (root.knownComponentDrawKinds.indexOf(drawKind) < 0)
                drawKind = "measured";

            var drawW = (c.draw_w === undefined || c.draw_w === null) ? null : Number(c.draw_w);
            if (!isFinite(drawW))
                drawW = null;

            var maxW = (c.max_w === undefined || c.max_w === null) ? null : Number(c.max_w);
            if (!isFinite(maxW) || maxW <= 0)
                maxW = null;

            var norm = {
                "id": id,
                "label": c.label !== undefined ? String(c.label) : id.toUpperCase(),
                "draw_kind": drawKind,
                "draw_source": c.draw_source !== undefined ? String(c.draw_source) : "",
                "draw_w": drawW,
                "max_w": maxW,
                "utilization_percent": clampInt(c.utilization_percent, 0, 100),
                "activity_percent": clampInt(c.activity_percent, 0, 100),
                "uncertainty_percent": clampInt(c.uncertainty_percent, 0, 100),
                "model": c.model
            };
            if (c.top_process && typeof c.top_process === "object")
                norm.top_process = c.top_process;

            out.push(norm);
        }
        return out;
    }

    function recordInvalid(reason) {
        invalidLineCount++;
        if (invalidLineCount >= 5) {
            stale = true;
            faultText = "Invalid telemetry";
        }
        if (!warnTimer.running) {
            console.warn("UTH Power: " + reason);
            warnTimer.start();
        }
    }

    function handleLine(line) {
        if (!line || !line.length)
            return ;

        if (line.length > root.maxLineBytes) {
            recordInvalid("line exceeds " + root.maxLineBytes + " bytes");
            return ;
        }
        var parsed;
        try {
            parsed = JSON.parse(String(line));
        } catch (e) {
            recordInvalid("failed to parse collector line: " + e);
            return ;
        }
        if (!root.validateTelemetry(parsed)) {
            recordInvalid("schema validation failed");
            return ;
        }
        // Valid sample: update state and track process health.
        invalidLineCount = 0;
        markHealthy();
        lastValidSampleTime = Date.now();
        stale = false;
        watchdog.restart();
        var nextSource = parsed.source;
        sourceData = nextSource;
        componentsData = root.normalizeComponents(parsed.components);
        dataLoaded = true;
    }

    function statusTooltip() {
        var status = root.permanentFailure ? "ERROR" : (root.retrying ? "RETRYING" : (root.stale ? "STALE" : (root.dataLoaded ? "LIVE" : "OFFLINE")));
        var text = "UTH Power — " + status;
        if (root.faultText)
            text += "\n" + root.faultText;

        return text;
    }

    moduleName: "uth.power"
    ipcTarget: "uth.power"
    onOpenedChanged: {
        if (opened) {
            resetFailureBudget();
            stale = false;
            if (collector.running && expectedStop)
                restartPending = true;
            else if (!collector.running)
                startCollector();
        } else {
            stopCollector("user");
            stale = false;
            retrying = false;
            restartPending = false;
        }
    }
    Component.onCompleted: {
        console.info("UTH Power: plugin loaded; binary path: " + root.binaryPath);
    }
    implicitWidth: button.implicitWidth
    implicitHeight: button.implicitHeight

    Process {
        id: collector

        command: [root.binaryPath]
        onStarted: {
            root.processStarted = true;
            root.lastProcessId = Number(root.processId) || 0;
            root.stderrTail = "";
        }
        onExited: function(exitCode, exitStatus) {
            root.onCollectorExited(exitCode, exitStatus);
        }

        stdout: SplitParser {
            onRead: function(line) {
                root.handleLine(line);
            }
        }

        stderr: SplitParser {
            onRead: function(line) {
                root.appendStderr(line);
            }
        }

    }

    Connections {
        function onRunningChanged() {
            if (!collector.running && root.opened && !root.expectedStop && !root.processStarted)
                root.onCollectorError(0);

        }

        target: collector
    }

    Process {
        id: killer

        function kill(pid) {
            if (!pid)
                return ;

            command = ["kill", "-9", String(pid)];
            running = true;
        }

        running: false
    }

    Timer {
        id: retryTimer

        repeat: false
        onTriggered: {
            if (collector.running)
                return ;

            root.startCollector();
        }
    }

    Timer {
        id: watchdog

        interval: 4000
        repeat: false
        running: root.opened && collector.running
        onTriggered: {
            root.consecutiveFailures++;
            root.faultText = "Collector unresponsive";
            root.stopCollector("failure");
            if (root.opened)
                root.scheduleRetry();

        }
    }

    Timer {
        id: killTimer

        interval: 2000
        repeat: false
        onTriggered: root.killProcess()
    }

    Timer {
        id: warnTimer

        interval: 5000
        repeat: false
    }

    BarIconButton {
        id: button

        anchors.fill: parent
        bar: root.bar
        text: "\uf0e7" // nf-fa-bolt
        tooltipText: root.statusTooltip()
        active: root.permanentFailure || root.retrying
        onPressed: function(b) {
            if (root.permanentFailure) {
                root.resetFailureBudget();
                root.startCollector();
            }
            root.toggle();
        }
    }

    KeyboardPanel {
        id: panel

        anchorItem: button
        owner: root
        bar: root.bar
        open: root.opened
        focusTarget: keyCatcher
        contentWidth: panel.fittedContentWidth(dashboard.implicitWidth + panel.padding * 2 + Border.left(panel.borderSpec) + Border.right(panel.borderSpec), Style.space(560))
        contentHeight: panel.fittedContentHeight(contentColumn.implicitHeight, Style.space(520))

        PanelKeyCatcher {
            id: keyCatcher

            anchors.fill: parent
            onCloseRequested: root.close()
            onTabRequested: function(direction) {
                root.switchPanel(direction);
            }

            Column {
                id: contentColumn

                width: parent.width
                spacing: Style.space(16)

                UthPowerWidget {
                    id: dashboard

                    width: parent.width
                    sourceData: root.sourceData
                    componentsData: root.componentsData
                    canonicalIds: root.canonicalIds
                    dataLoaded: root.dataLoaded
                    stale: root.stale
                    retrying: root.retrying
                    permanentFailure: root.permanentFailure
                    faultText: root.faultText
                    reduceMotion: root.reduceMotion
                    foreground: root.bar ? root.bar.foreground : Commons.Color.foreground
                    dimmed: root.bar ? Qt.darker(root.bar.foreground, 1.35) : Qt.darker(Commons.Color.foreground, 1.35)
                    accent: Commons.Color.accent
                    urgent: root.bar ? root.bar.urgent : Commons.Color.urgent
                    fontFamily: root.bar ? root.bar.fontFamily : Style.font.family
                }

            }

        }

    }

}
