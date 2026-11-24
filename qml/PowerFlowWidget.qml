// Main dashboard for the Omarchy Power Flow popup.

import QtQuick
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// Rich power-flow dashboard: header with status and provenance legend,
// a source hub on the left, and per-component tiles on the right. Cords
// run behind the tiles from source to each consumer.
Item {
    id: root

    required property var sourceData
    required property var componentsData
    required property var canonicalIds
    property bool dataLoaded: false
    property bool stale: false
    property bool retrying: false
    property bool permanentFailure: false
    property string faultText: ""
    property bool reduceMotion: false
    property color foreground: Commons.Color.foreground
    property color dimmed: Qt.darker(foreground, 1.35)
    property color accent: Commons.Color.accent
    property color urgent: Commons.Color.urgent
    property string fontFamily: Style.font.family
    property string currentView: "components"
    readonly property var processTileSpecs: [
        { "hub": "cpu", "index": 0 },
        { "hub": "cpu", "index": 1 },
        { "hub": "ram", "index": 0 },
        { "hub": "ram", "index": 1 }
    ]
    readonly property int bodySpacing: Style.space(26)
    readonly property int nodeSpacing: Style.space(10)
    readonly property int cardPadding: Style.space(12)
    readonly property int portSize: Style.space(10)
    readonly property int headerBodyGap: Style.space(14)
    readonly property string dominantId: {
        var bestId = "";
        var bestW = -1;
        for (var i = 0; i < root.canonicalIds.length; i++) {
            var id = root.canonicalIds[i];
            var c = root.componentById(id);
            if (!c)
                continue;
            var w = c.draw_w !== undefined && c.draw_w !== null && isFinite(Number(c.draw_w)) ? Number(c.draw_w) : 0;
            if (w > 0 && w > bestW) {
                bestW = w;
                bestId = id;
            }
        }
        if (bestId !== "")
            return bestId;

        for (var i = 0; i < root.canonicalIds.length; i++) {
            id = root.canonicalIds[i];
            c = root.componentById(id);
            if (!c)
                continue;
            var pct = 0;
            if (id === "cpu" || id === "ram")
                pct = typeof c.utilization_percent === "number" ? c.utilization_percent : 0;
            else
                pct = typeof c.activity_percent === "number" ? c.activity_percent : 0;
            if (pct > 0 && pct > bestW) {
                bestW = pct;
                bestId = id;
            }
        }
        return bestId;
    }
    property var nodeMap: ({
    })

    function registerNode(id, item) {
        var next = {
        };
        for (var k in nodeMap) next[k] = nodeMap[k]
        next[id] = item;
        nodeMap = next;
    }

    function unregisterNode(id, item) {
        if (nodeMap[id] !== item)
            return ;

        var next = {
        };
        for (var k in nodeMap) {
            if (k !== id)
                next[k] = nodeMap[k];

        }
        nodeMap = next;
    }

    function componentById(id) {
        for (var i = 0; i < componentsData.length; i++) {
            var c = componentsData[i];
            if (c && c.id === id)
                return c;

        }
        return null;
    }

    // Last non-empty process lists per hub.  The collector omits the lists on
    // incomplete (over-budget) scans, so tiles hold the previous sample
    // instead of flashing "unavailable" on a gap.
    property var cachedCpuProcs: []
    property var cachedRamProcs: []
    onComponentsDataChanged: root.refreshProcCache()
    Component.onCompleted: root.refreshProcCache()

    function refreshProcCache() {
        var cpu = root.collectTopProcesses("cpu");
        if (cpu.length > 0)
            cachedCpuProcs = cpu;

        var ram = root.collectTopProcesses("ram");
        if (ram.length > 0)
            cachedRamProcs = ram;

    }

    function topProcessesFor(id) {
        var fresh = root.collectTopProcesses(id);
        if (fresh.length > 0)
            return fresh;

        if (id === "cpu" && cachedCpuProcs.length > 0)
            return cachedCpuProcs;

        if (id === "ram" && cachedRamProcs.length > 0)
            return cachedRamProcs;

        return fresh;
    }

    function collectTopProcesses(id) {
        var c = componentById(id);
        if (!c)
            return [];
        var list = [];
        if (Array.isArray(c.top_processes)) {
            for (var i = 0; i < c.top_processes.length && list.length < 2; i++) {
                var p = c.top_processes[i];
                if (p && typeof p === "object")
                    list.push(p);

            }
        }
        if (list.length === 0 && c.top_process && typeof c.top_process === "object")
            list.push(c.top_process);
        return list;
    }

    function sourceName() {
        var mode = sourceData && sourceData.power_mode ? String(sourceData.power_mode).toLowerCase() : "";
        if (mode === "unknown")
            return "System";

        if (sourceData && sourceData.name)
            return String(sourceData.name);

        return "System";
    }

    function sourceGlyph() {
        var mode = sourceData && sourceData.power_mode ? String(sourceData.power_mode).toLowerCase() : "";
        if (!mode) {
            // Legacy fallback by source name substring.
            var name = sourceName().toLowerCase();
            if (name.indexOf("battery") >= 0)
                mode = "battery";
            else if (name.indexOf("ac") >= 0 || name.indexOf("power") >= 0 || name.indexOf("plug") >= 0)
                mode = "ac";
            else
                mode = "unknown";
        }
        if (mode === "ac")
            return "\uf1e6";

        if (mode === "battery") {
            var status = sourceData && sourceData.battery_status ? String(sourceData.battery_status).toLowerCase() : "";
            if (status === "charging" || status === "full" || status === "ac")
                return "\uf0e7";

            var pct = sourceData && typeof sourceData.battery_percentage === "number" ? sourceData.battery_percentage : -1;
            if (pct >= 75)
                return "\uf240";

            if (pct >= 50)
                return "\uf241";

            if (pct >= 25)
                return "\uf242";

            if (pct > 0)
                return "\uf243";

            return "\uf244";
        }
        return "\uf0e7";
    }

    function componentGlyph(id) {
        if (id === "cpu")
            return "\uf2db";

        if (id === "gpu")
            return "\uf108";

        if (id === "ram")
            return "\uf233";

        if (id === "disk")
            return "\uf0a0";

        return "\uf0e7";
    }

    function formatBytes(bytes) {
        var n = Number(bytes);
        if (!isFinite(n) || n < 0)
            return "—";

        if (n < 1024)
            return n.toFixed(0) + " B";

        if (n < 1024 * 1024)
            return (n / 1024).toFixed(1) + " KiB";

        if (n < 1024 * 1024 * 1024)
            return (n / (1024 * 1024)).toFixed(1) + " MiB";

        return (n / (1024 * 1024 * 1024)).toFixed(1) + " GiB";
    }

    function formatSourceTotal() {
        var kind = sourceData && sourceData.total_draw_kind ? String(sourceData.total_draw_kind) : "none";
        if (!root.dataLoaded || kind === "none")
            return "—";

        var prefix = (kind === "modeled" || kind === "mixed") ? "~" : "";
        var n = Number(sourceData && sourceData.total_draw_w);
        if (!isFinite(n))
            return prefix + "—";

        if (n < 10)
            return prefix + n.toLocaleString(Qt.locale(), "f", 1) + " W";

        return prefix + Math.round(n).toLocaleString(Qt.locale(), "f", 0) + " W";
    }

    function statusText() {
        if (root.permanentFailure)
            return "ERROR";

        if (root.retrying)
            return "RETRYING";

        if (root.stale)
            return "STALE";

        if (root.dataLoaded)
            return "LIVE";

        return "OFFLINE";
    }

    function statusColor() {
        if (root.permanentFailure)
            return root.urgent;

        if (root.retrying)
            return root.accent;

        if (root.stale)
            return root.urgent;

        if (root.dataLoaded)
            return root.accent;

        return root.dimmed;
    }

    implicitWidth: Math.max(header.implicitWidth, body.implicitWidth, processesBody.implicitWidth)
    implicitHeight: header.implicitHeight + root.headerBodyGap + Math.max(body.implicitHeight, processesBody.implicitHeight)

    // ---------- Header: identity, status, legend ----------
    Column {
        id: header

        spacing: Style.space(4)
        anchors.left: root.currentView === "processes" ? processesBody.left : body.left
        width: root.currentView === "processes" ? processesBody.width : body.width

        Row {
            spacing: Style.space(10)

            Text {
                text: "\uf0e7"
                color: root.accent
                font.family: root.fontFamily
                font.pixelSize: Style.font.title
                anchors.verticalCenter: parent.verticalCenter
            }

            Column {
                spacing: Style.space(1)
                anchors.verticalCenter: parent.verticalCenter

                Text {
                    text: "POWER FLOW"
                    color: root.foreground
                    font.family: root.fontFamily
                    font.pixelSize: Style.font.title
                    font.bold: true
                }

                Row {
                    spacing: Style.space(6)

                    Text {
                        text: root.statusText()
                        color: root.statusColor()
                        font.family: root.fontFamily
                        font.pixelSize: Style.font.caption
                        font.bold: true
                    }

                    Text {
                        text: "·"
                        color: root.dimmed
                        font.family: root.fontFamily
                        font.pixelSize: Style.font.caption
                    }

                    Text {
                        text: root.currentView === "processes" ? "activity share, not watts" : "~ estimated  ·  exact measured"
                        color: root.dimmed
                        font.family: root.fontFamily
                        font.pixelSize: Style.font.caption
                    }

                }

            }

        }

        Row {
            spacing: Style.space(14)

            ViewTab {
                label: "COMPONENTS"
                active: root.currentView === "components"
                onActivated: root.currentView = "components"
            }

            ViewTab {
                label: "PROCESSES"
                active: root.currentView === "processes"
                onActivated: root.currentView = "processes"
            }

        }

        Text {
            visible: root.faultText !== ""
            text: root.faultText
            color: root.urgent
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
            font.bold: true
            horizontalAlignment: Text.AlignHCenter
            anchors.horizontalCenter: parent.horizontalCenter
            wrapMode: Text.Wrap
            width: parent.width
        }

    }

    // ---------- Body: source hub, cords, consumer tiles ----------
    Item {
        id: body

        visible: root.currentView === "components"
        anchors.top: header.bottom
        anchors.topMargin: root.headerBodyGap
        anchors.horizontalCenter: parent.horizontalCenter
        implicitWidth: sourceHub.implicitWidth + root.bodySpacing + componentColumn.implicitWidth
        implicitHeight: componentColumn.implicitHeight
        width: implicitWidth
        height: implicitHeight

        SourceHub {
            id: sourceHub

            anchors.left: parent.left
            anchors.verticalCenter: parent.verticalCenter
            height: componentColumn.height
        }

        CordLayer {
            anchors.fill: parent
            z: -1
        }

        Column {
            id: componentColumn

            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            spacing: root.nodeSpacing

            Repeater {
                model: root.canonicalIds

                ComponentTile {
                    nodeId: modelData
                    Component.onCompleted: root.registerNode(nodeId, this)
                    Component.onDestruction: root.unregisterNode(nodeId, this)
                }

            }

        }

    }

    // ---------- Processes body: mini hubs, cords, process tiles ----------
    Item {
        id: processesBody

        visible: root.currentView === "processes"
        anchors.top: header.bottom
        anchors.topMargin: root.headerBodyGap
        anchors.horizontalCenter: parent.horizontalCenter
        implicitWidth: hubStack.implicitWidth + root.bodySpacing + processColumn.implicitWidth
        implicitHeight: Math.max(hubStack.implicitHeight, processColumn.implicitHeight)
        width: implicitWidth
        height: implicitHeight

        ProcessCordLayer {
            anchors.fill: parent
            z: -1
        }

        Column {
            id: hubStack

            anchors.left: parent.left
            anchors.verticalCenter: parent.verticalCenter
            spacing: root.nodeSpacing

            MiniHub {
                id: cpuHub

                hubId: "cpu"
            }

            MiniHub {
                id: ramHub

                hubId: "ram"
            }

        }

        Column {
            id: processColumn

            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            spacing: root.nodeSpacing

            Repeater {
                model: root.processTileSpecs

                ProcessTile {
                    required property var modelData
                    tileId: modelData.hub + "-proc-" + modelData.index
                    hubId: modelData.hub
                    procIndex: modelData.index
                    Component.onCompleted: root.registerNode(tileId, this)
                    Component.onDestruction: root.unregisterNode(tileId, this)
                }

            }

        }

    }

    component SourceHub: BorderSurface {
        id: hub

        width: Math.max(Style.space(124), hubColumn.implicitWidth + root.cardPadding * 2)
        height: parent.height
        implicitWidth: width
        implicitHeight: height
        color: Util.alpha(root.accent, 0.06)
        borderSpec: Border.surfaceSpec("popups", "border", Commons.Color.popups.border, Math.max(1, Style.space(1)))
        radius: Style.cornerRadius
        padding: 0

        Column {
            id: hubColumn

            anchors.centerIn: parent
            spacing: Style.space(6)
            width: parent.width - root.cardPadding * 2

            Text {
                text: root.sourceGlyph()
                color: root.stale ? root.dimmed : root.accent
                font.family: root.fontFamily
                font.pixelSize: Style.font.display
                horizontalAlignment: Text.AlignHCenter
                anchors.horizontalCenter: parent.horizontalCenter
            }

            Text {
                text: root.sourceName().toUpperCase()
                color: root.stale ? root.dimmed : root.foreground
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
                font.letterSpacing: 1.2
                horizontalAlignment: Text.AlignHCenter
                anchors.horizontalCenter: parent.horizontalCenter
            }

            Text {
                text: root.dataLoaded ? (root.stale ? "—" : root.formatSourceTotal()) : "—"
                color: root.dataLoaded ? (root.stale ? root.dimmed : root.accent) : root.dimmed
                font.family: root.fontFamily
                font.pixelSize: Style.font.display
                font.bold: true
                horizontalAlignment: Text.AlignHCenter
                anchors.horizontalCenter: parent.horizontalCenter
            }

            Text {
                visible: root.dataLoaded && !root.stale && root.sourceData && root.sourceData.battery_percentage >= 0
                text: root.sourceData.battery_percentage + "%"
                color: root.dimmed
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
                horizontalAlignment: Text.AlignHCenter
                anchors.horizontalCenter: parent.horizontalCenter
            }

            Text {
                visible: root.stale
                text: "STALE"
                color: root.urgent
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
                horizontalAlignment: Text.AlignHCenter
                anchors.horizontalCenter: parent.horizontalCenter
            }

        }

        // Output port where all cords originate.
        Rectangle {
            anchors.verticalCenter: parent.verticalCenter
            x: parent.width - width / 2
            width: Style.space(12)
            height: width
            radius: width / 2
            color: root.dataLoaded ? root.accent : root.foreground
            opacity: root.dataLoaded ? 0.9 : 0.35
            border.color: root.foreground
            border.width: Math.max(1, Style.spaceReal(1))
        }

    }

    component ComponentTile: BorderSurface {
        id: tile

        property string nodeId: ""
        readonly property var c: root.componentById(nodeId)
        readonly property bool available: !!c
        readonly property var drawW: c && c.draw_w !== undefined && c.draw_w !== null ? c.draw_w : null
        readonly property var maxW: c && c.max_w !== undefined && c.max_w !== null && c.max_w > 0 ? c.max_w : null
        readonly property string drawKind: c && c.draw_kind ? String(c.draw_kind) : "measured"
        readonly property string drawSource: c && c.draw_source ? String(c.draw_source) : ""
        readonly property int utilizationPercent: c && typeof c.utilization_percent === "number" ? c.utilization_percent : 0
        readonly property int activityPercent: c && typeof c.activity_percent === "number" ? c.activity_percent : 0
        readonly property int uncertaintyPercent: c && typeof c.uncertainty_percent === "number" ? c.uncertainty_percent : 0
        readonly property string modelName: c && c.model && typeof c.model === "object" && c.model.id !== undefined ? String(c.model.id) : ""
        readonly property var topProcess: c && c.top_process ? c.top_process : null
        readonly property bool hasWatts: drawW !== null && drawW !== undefined && isFinite(Number(drawW))
        readonly property bool isDominant: root.dominantId === tile.nodeId && root.dataLoaded && !root.stale
        readonly property real displayFraction: {
            if (nodeId === "cpu" || nodeId === "ram")
                return Math.max(0, Math.min(1, utilizationPercent / 100));

            return Math.max(0, Math.min(1, activityPercent / 100));
        }

        function formatWatts(w, kind) {
            if (w === null || w === undefined)
                return "—";

            var n = Number(w);
            if (!isFinite(n))
                return "—";

            var prefix = kind === "modeled" ? "~" : "";
            if (n < 10)
                return prefix + n.toLocaleString(Qt.locale(), "f", 1) + " W";

            return prefix + Math.round(n).toLocaleString(Qt.locale(), "f", 0) + " W";
        }

        function loadText() {
            if (!root.dataLoaded)
                return "—";

            if (!tile.available)
                return "unavailable";

            if (nodeId === "cpu")
                return utilizationPercent + "% load";

            if (nodeId === "ram")
                return utilizationPercent + "% used";

            return activityPercent + "%";
        }

        function topProcessText() {
            var tp = tile.topProcess;
            if (!tp || typeof tp !== "object")
                return "";

            var name = tp.name !== undefined ? String(tp.name) : "unknown";
            if (nodeId === "cpu") {
                var value = Number(tp.value);
                var core = Number(tp.core_percent);
                if (!isFinite(value))
                    return "";

                var text = "Top CPU: " + name + " — " + value.toFixed(1) + "% of machine";
                if (isFinite(core))
                    text += " (" + core.toFixed(0) + "% of one core)";

                return text;
            }
            if (nodeId === "ram") {
                var bytes = Number(tp.value);
                if (!isFinite(bytes))
                    return "";

                return "Largest resident: " + name + " — " + root.formatBytes(bytes) + " RSS";
            }
            return "";
        }

        function tooltipText() {
            var parts = [];
            var tp = tile.topProcessText();
            if (tp)
                parts.push(tp);

            if (drawSource)
                parts.push("Source: " + drawSource);

            if (modelName)
                parts.push("Model: " + modelName);

            if (uncertaintyPercent > 0)
                parts.push("Uncertainty: ±" + uncertaintyPercent + "%");

            return parts.join("\n");
        }

        width: Style.space(168)
        height: Style.space(68)
        implicitWidth: width
        implicitHeight: height
        color: Util.alpha(root.foreground, 0.04)
        borderSpec: Border.surfaceSpec("popups", "border", Commons.Color.popups.border, Math.max(1, Style.space(1)))
        radius: Style.cornerRadius
        padding: 0

        Row {
            anchors.fill: parent
            anchors.margins: root.cardPadding
            spacing: Style.space(10)

            Text {
                text: root.componentGlyph(tile.nodeId)
                color: tile.available ? root.accent : root.dimmed
                font.family: root.fontFamily
                font.pixelSize: Style.font.title
                anchors.verticalCenter: parent.verticalCenter
            }

            Column {
                spacing: Style.space(1)
                anchors.verticalCenter: parent.verticalCenter
                width: parent.width - Style.space(24)

                Text {
                    text: tile.c && tile.c.label ? String(tile.c.label) : tile.nodeId.toUpperCase()
                    color: tile.available ? root.foreground : root.dimmed
                    font.family: root.fontFamily
                    font.pixelSize: Style.font.body
                    font.bold: true
                }

                Text {
                    text: tile.available ? tile.formatWatts(tile.drawW, tile.drawKind) : "unavailable"
                    color: tile.available && tile.hasWatts && Number(tile.drawW) > 0 ? root.accent : (tile.available ? root.foreground : root.dimmed)
                    font.family: root.fontFamily
                    font.pixelSize: Style.font.bodySmall
                    font.bold: true
                }

                Text {
                    text: tile.loadText()
                    color: root.dimmed
                    font.family: root.fontFamily
                    font.pixelSize: Style.font.caption
                }

            }

        }

        // Bottom activity meter.
        Rectangle {
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.bottom: parent.bottom
            anchors.leftMargin: root.cardPadding
            anchors.rightMargin: root.cardPadding
            anchors.bottomMargin: root.cardPadding / 2
            height: Math.max(Style.space(3), Style.spacing.xs)
            radius: height / 2
            color: Util.alpha(root.foreground, 0.15)

            Rectangle {
                anchors.left: parent.left
                anchors.verticalCenter: parent.verticalCenter
                height: parent.height
                radius: parent.radius
                width: Math.max(height, parent.width * tile.displayFraction)
                color: tile.displayFraction > 0 ? root.accent : Util.alpha(root.foreground, 0.35)

                Behavior on width {
                    NumberAnimation {
                        duration: 300
                        easing.type: Easing.OutCubic
                    }

                }

            }

        }

        // Input port where the cord terminates.
        Rectangle {
            anchors.verticalCenter: parent.verticalCenter
            x: -width / 2
            width: root.portSize
            height: width
            radius: width / 2
            color: tile.available ? root.accent : root.dimmed
            opacity: tile.available ? 0.9 : 0.25
            border.color: root.foreground
            border.width: tile.available ? Math.max(1, Style.spaceReal(1)) : 0
        }

        // Dominant-tile pulse border.
        Rectangle {
            id: pulseBorder

            anchors.fill: parent
            anchors.margins: -Style.spaceReal(1.5)
            radius: Style.cornerRadius + Style.spaceReal(1.5)
            color: "transparent"
            visible: tile.isDominant

            property real pulseProgress: 0
            readonly property real pulseAlpha: root.reduceMotion ? 0.85 : 0.4 + 0.6 * pulseProgress
            readonly property real pulseWidth: root.reduceMotion ? Math.max(1, Style.spaceReal(1.5)) : Math.max(1, Style.spaceReal(1) + Style.spaceReal(1) * pulseProgress)

            border.color: Util.alpha(root.accent, pulseAlpha)
            border.width: pulseWidth

            SequentialAnimation {
                loops: Animation.Infinite
                running: tile.isDominant && !root.reduceMotion

                NumberAnimation {
                    target: pulseBorder
                    property: "pulseProgress"
                    from: 0
                    to: 1
                    duration: 1100
                    easing.type: Easing.InOutSine
                }

                NumberAnimation {
                    target: pulseBorder
                    property: "pulseProgress"
                    from: 1
                    to: 0
                    duration: 1100
                    easing.type: Easing.InOutSine
                }

            }

        }

        // Provenance tooltip.
        MouseArea {
            id: tileHover

            anchors.fill: parent
            hoverEnabled: true
            acceptedButtons: Qt.NoButton

            PanelToolTip {
                id: tileTooltip

                visible: tileHover.containsMouse && tile.tooltipText() !== ""
                text: tile.tooltipText()
                fontFamily: root.fontFamily
            }

        }

    }

    component CordLayer: Item {
        id: cordLayerRoot

        readonly property real sourceTotalW: root.sourceData && isFinite(Number(root.sourceData.total_draw_w)) ? Number(root.sourceData.total_draw_w) : 0
        readonly property real maxComponentDrawW: {
            var max = 0;
            for (var i = 0; i < root.canonicalIds.length; i++) {
                var c = root.componentById(root.canonicalIds[i]);
                var w = c && c.draw_w !== undefined && c.draw_w !== null && isFinite(Number(c.draw_w)) ? Number(c.draw_w) : 0;
                if (w > max)
                    max = w;

            }
            return max;
        }
        readonly property real maxShare: cordLayerRoot.sourceTotalW > 0 ? cordLayerRoot.maxComponentDrawW / cordLayerRoot.sourceTotalW : 0

        Repeater {
            model: root.canonicalIds

            AnimatedCord {
                required property string modelData
                readonly property var endNode: root.nodeMap[modelData] || null
                readonly property var endData: root.componentById(modelData)
                readonly property real endDrawW: endData && endData.draw_w !== undefined && endData.draw_w !== null && isFinite(Number(endData.draw_w)) ? Number(endData.draw_w) : 0

                anchors.fill: parent
                startX: sourceHub ? sourceHub.x + sourceHub.width : 0
                startY: sourceHub ? sourceHub.y + sourceHub.height / 2 : 0
                endX: endNode && componentColumn ? componentColumn.x + endNode.x : startX
                endY: endNode && componentColumn ? componentColumn.y + endNode.y + endNode.height / 2 : startY
                drawW: endData && endData.draw_w !== undefined && endData.draw_w !== null ? endData.draw_w : null
                maxW: endData && endData.max_w !== undefined && endData.max_w !== null && endData.max_w > 0 ? endData.max_w : null
                drawKind: endData && endData.draw_kind ? String(endData.draw_kind) : "measured"
                utilizationPercent: endData && typeof endData.utilization_percent === "number" ? endData.utilization_percent : 0
                activityPercent: endData && typeof endData.activity_percent === "number" ? endData.activity_percent : 0
                sourceTotalW: cordLayerRoot.sourceTotalW
                maxShare: cordLayerRoot.maxShare
                isDominant: cordLayerRoot.sourceTotalW > 0 && endDrawW >= cordLayerRoot.maxComponentDrawW * 0.999
                available: !!endData
                dataLoaded: root.dataLoaded
                stale: root.stale
                reduceMotion: root.reduceMotion
                foreground: root.foreground
                dimmed: root.dimmed
                accent: root.accent
            }

        }

    }

    component ViewTab: Item {
        id: viewTab

        property string label: ""
        property bool active: false
        signal activated()

        width: viewTabText.implicitWidth + Style.space(14)
        height: viewTabText.implicitHeight + Style.space(8)
        implicitWidth: width
        implicitHeight: height

        Text {
            id: viewTabText

            anchors.centerIn: parent
            text: viewTab.label
            color: viewTab.active ? root.foreground : root.dimmed
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
            font.bold: true
            font.letterSpacing: 1.2
        }

        Rectangle {
            anchors.horizontalCenter: parent.horizontalCenter
            anchors.bottom: parent.bottom
            width: parent.width - Style.space(8)
            height: Math.max(1, Style.spaceReal(1.5))
            radius: height / 2
            color: root.accent
            visible: viewTab.active
        }

        MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: viewTab.activated()
        }

    }

    component MiniHub: BorderSurface {
        id: miniHub

        property string hubId: ""
        readonly property var c: root.componentById(hubId)
        readonly property bool available: !!c
        readonly property var drawW: c && c.draw_w !== undefined && c.draw_w !== null ? c.draw_w : null
        readonly property string drawKind: c && c.draw_kind ? String(c.draw_kind) : "measured"
        readonly property int utilizationPercent: c && typeof c.utilization_percent === "number" ? c.utilization_percent : 0
        readonly property bool hasWatts: drawW !== null && drawW !== undefined && isFinite(Number(drawW))

        function valueText() {
            if (!root.dataLoaded)
                return "—";

            if (!miniHub.available)
                return "unavailable";

            if (drawW === null || drawW === undefined || !isFinite(Number(drawW)))
                return "—";

            var n = Number(drawW);
            var prefix = drawKind === "modeled" ? "~" : "";
            if (n < 10)
                return prefix + n.toLocaleString(Qt.locale(), "f", 1) + " W";

            return prefix + Math.round(n).toLocaleString(Qt.locale(), "f", 0) + " W";
        }

        function loadText() {
            if (!root.dataLoaded)
                return "—";

            if (!miniHub.available)
                return "unavailable";

            if (hubId === "cpu")
                return utilizationPercent + "% load";

            return utilizationPercent + "% used";
        }

        // Match the components-view hub width so switching views does not
        // shift the panel: same left edge, same cords origin geometry.
        width: Math.max(sourceHub.width, miniHubColumn.implicitWidth + root.cardPadding * 2)
        height: Style.space(104)
        implicitWidth: width
        implicitHeight: height
        color: Util.alpha(root.accent, 0.06)
        borderSpec: Border.surfaceSpec("popups", "border", Commons.Color.popups.border, Math.max(1, Style.space(1)))
        radius: Style.cornerRadius
        padding: 0

        Column {
            id: miniHubColumn

            anchors.centerIn: parent
            spacing: Style.space(4)

            Text {
                text: root.componentGlyph(miniHub.hubId)
                color: root.stale ? root.dimmed : root.accent
                font.family: root.fontFamily
                font.pixelSize: Style.font.title
                horizontalAlignment: Text.AlignHCenter
                anchors.horizontalCenter: parent.horizontalCenter
            }

            Text {
                text: miniHub.hubId.toUpperCase()
                color: root.stale ? root.dimmed : root.foreground
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
                font.letterSpacing: 1.2
                horizontalAlignment: Text.AlignHCenter
                anchors.horizontalCenter: parent.horizontalCenter
            }

            Text {
                text: miniHub.valueText()
                color: miniHub.available && miniHub.hasWatts && Number(miniHub.drawW) > 0 ? root.accent : (miniHub.available ? root.foreground : root.dimmed)
                font.family: root.fontFamily
                font.pixelSize: Style.font.bodySmall
                font.bold: true
                horizontalAlignment: Text.AlignHCenter
                anchors.horizontalCenter: parent.horizontalCenter
            }

            Text {
                text: miniHub.loadText()
                color: root.dimmed
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                horizontalAlignment: Text.AlignHCenter
                anchors.horizontalCenter: parent.horizontalCenter
            }

            Text {
                visible: root.stale
                text: "STALE"
                color: root.urgent
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
                horizontalAlignment: Text.AlignHCenter
                anchors.horizontalCenter: parent.horizontalCenter
            }

        }

        // Output port where cords originate.
        Rectangle {
            anchors.verticalCenter: parent.verticalCenter
            x: parent.width - width / 2
            width: root.portSize
            height: width
            radius: width / 2
            color: root.dataLoaded ? root.accent : root.foreground
            opacity: root.dataLoaded ? 0.9 : 0.35
            border.color: root.foreground
            border.width: Math.max(1, Style.spaceReal(1))
        }

    }

    component ProcessTile: BorderSurface {
        id: procTile

        property string tileId: ""
        property string hubId: ""
        property int procIndex: 0
        readonly property bool isCpu: hubId === "cpu"
        readonly property var procs: root.topProcessesFor(hubId)
        readonly property var proc: procs.length > procIndex && procs[procIndex] && typeof procs[procIndex] === "object" ? procs[procIndex] : null
        readonly property bool active: root.dataLoaded && !root.stale && proc !== null
        readonly property real procValue: proc && proc.value !== undefined && proc.value !== null ? Number(proc.value) : NaN
        readonly property real listMaxValue: {
            var max = 0;
            for (var i = 0; i < procs.length; i++) {
                var v = procs[i] && procs[i].value !== undefined ? Number(procs[i].value) : NaN;
                if (isFinite(v) && v > max)
                    max = v;

            }
            return max;
        }
        // CPU meters scale the machine-wide percent directly; RAM meters
        // normalize against the largest resident set in the list.
        readonly property real displayFraction: {
            if (!procTile.active)
                return 0;

            if (isCpu)
                return Math.max(0, Math.min(1, procValue / 100));

            return listMaxValue > 0 ? Math.max(0, Math.min(1, procValue / listMaxValue)) : 0;
        }

        function titleText() {
            if (!procTile.active)
                return "—";

            var name = proc.name !== undefined && proc.name !== null ? String(proc.name) : "";
            return name !== "" ? name : "unknown";
        }

        function valueText() {
            if (!root.dataLoaded || root.stale)
                return "—";

            if (!proc)
                return "unavailable";

            if (!isFinite(procValue))
                return "—";

            if (isCpu) {
                var text = procValue.toFixed(1) + "% of machine";
                var core = Number(proc.core_percent);
                if (isFinite(core))
                    text += " (" + core.toFixed(0) + "% of one core)";

                return text;
            }
            return root.formatBytes(procValue) + " RSS";
        }

        function pidText() {
            if (!procTile.active)
                return "";

            var pid = Number(proc.pid);
            return isFinite(pid) ? "PID " + Math.round(pid) : "";
        }

        width: Style.space(168)
        height: Style.space(68)
        implicitWidth: width
        implicitHeight: height
        color: Util.alpha(root.foreground, 0.04)
        borderSpec: Border.surfaceSpec("popups", "border", Commons.Color.popups.border, Math.max(1, Style.space(1)))
        radius: Style.cornerRadius
        padding: 0

        Row {
            anchors.fill: parent
            anchors.margins: root.cardPadding
            spacing: Style.space(10)

            Text {
                text: root.componentGlyph(procTile.hubId)
                color: procTile.active ? root.accent : root.dimmed
                font.family: root.fontFamily
                font.pixelSize: Style.font.title
                anchors.verticalCenter: parent.verticalCenter
            }

            Column {
                spacing: Style.space(1)
                anchors.verticalCenter: parent.verticalCenter
                width: parent.width - Style.space(24)

                Text {
                    width: parent.width
                    text: procTile.titleText()
                    color: procTile.active ? root.foreground : root.dimmed
                    font.family: root.fontFamily
                    font.pixelSize: Style.font.body
                    font.bold: true
                    elide: Text.ElideRight
                }

                Text {
                    width: parent.width
                    text: procTile.valueText()
                    color: procTile.active ? root.accent : root.dimmed
                    font.family: root.fontFamily
                    font.pixelSize: Style.font.bodySmall
                    font.bold: true
                    elide: Text.ElideRight
                }

                Text {
                    width: parent.width
                    text: procTile.pidText()
                    color: root.dimmed
                    font.family: root.fontFamily
                    font.pixelSize: Style.font.caption
                    elide: Text.ElideRight
                }

            }

        }

        // Bottom activity meter.
        Rectangle {
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.bottom: parent.bottom
            anchors.leftMargin: root.cardPadding
            anchors.rightMargin: root.cardPadding
            anchors.bottomMargin: root.cardPadding / 2
            height: Math.max(Style.space(3), Style.spacing.xs)
            radius: height / 2
            color: Util.alpha(root.foreground, 0.15)

            Rectangle {
                anchors.left: parent.left
                anchors.verticalCenter: parent.verticalCenter
                height: parent.height
                radius: parent.radius
                width: Math.max(height, parent.width * procTile.displayFraction)
                color: procTile.displayFraction > 0 ? root.accent : Util.alpha(root.foreground, 0.35)

                Behavior on width {
                    NumberAnimation {
                        duration: 300
                        easing.type: Easing.OutCubic
                    }

                }

            }

        }

        // Input port where the cord terminates.
        Rectangle {
            anchors.verticalCenter: parent.verticalCenter
            x: -width / 2
            width: root.portSize
            height: width
            radius: width / 2
            color: procTile.active ? root.accent : root.dimmed
            opacity: procTile.active ? 0.9 : 0.25
            border.color: root.foreground
            border.width: procTile.active ? Math.max(1, Style.spaceReal(1)) : 0
        }

    }

    component ProcessCordLayer: Item {
        id: processCordLayerRoot

        Repeater {
            model: root.processTileSpecs

            AnimatedCord {
                required property var modelData
                readonly property string hubId: modelData.hub
                readonly property int procIndex: modelData.index
                readonly property string tileId: hubId + "-proc-" + procIndex
                readonly property var endNode: root.nodeMap[tileId] || null
                readonly property var hubNode: hubId === "cpu" ? cpuHub : ramHub
                readonly property var procs: root.topProcessesFor(hubId)
                readonly property var proc: procs.length > procIndex && procs[procIndex] && typeof procs[procIndex] === "object" ? procs[procIndex] : null
                readonly property bool hasProc: proc !== null
                readonly property real procValue: proc && proc.value !== undefined && proc.value !== null ? Number(proc.value) : NaN
                readonly property real listMaxValue: {
                    var max = 0;
                    for (var i = 0; i < procs.length; i++) {
                        var v = procs[i] && procs[i].value !== undefined ? Number(procs[i].value) : NaN;
                        if (isFinite(v) && v > max)
                            max = v;

                    }
                    return max;
                }
                readonly property int cordUtilPercent: hubId === "cpu" && isFinite(procValue) ? Math.round(Math.max(0, Math.min(100, procValue))) : 0
                readonly property int cordActivityPercent: hubId === "cpu" ? cordUtilPercent : (isFinite(procValue) && listMaxValue > 0 ? Math.round(Math.max(0, Math.min(100, procValue / listMaxValue * 100))) : 0)

                anchors.fill: parent
                startX: hubNode ? hubStack.x + hubNode.x + hubNode.width : 0
                startY: hubNode ? hubStack.y + hubNode.y + hubNode.height / 2 : startY
                endX: endNode && processColumn ? processColumn.x + endNode.x : startX
                endY: endNode && processColumn ? processColumn.y + endNode.y + endNode.height / 2 : startY
                drawW: null
                maxW: null
                sourceTotalW: 0
                maxShare: 0
                isDominant: false
                drawKind: "measured"
                utilizationPercent: cordUtilPercent
                activityPercent: cordActivityPercent
                available: hasProc
                dataLoaded: root.dataLoaded
                stale: root.stale
                reduceMotion: root.reduceMotion
                foreground: root.foreground
                dimmed: root.dimmed
                accent: root.accent
            }

        }

    }

}
