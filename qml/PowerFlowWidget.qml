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
    readonly property int bodySpacing: Style.space(26)
    readonly property int nodeSpacing: Style.space(10)
    readonly property int cardPadding: Style.space(12)
    readonly property int portSize: Style.space(10)
    readonly property int headerBodyGap: Style.space(14)
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

    function sourceName() {
        if (sourceData && sourceData.name)
            return String(sourceData.name);

        return "Unknown";
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

    implicitWidth: Math.max(header.implicitWidth, body.implicitWidth)
    implicitHeight: header.implicitHeight + root.headerBodyGap + body.implicitHeight

    // ---------- Header: identity, status, legend ----------
    Column {
        id: header

        spacing: Style.space(4)
        anchors.horizontalCenter: parent.horizontalCenter

        Row {
            spacing: Style.space(10)
            anchors.horizontalCenter: parent.horizontalCenter

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
                        text: "~ estimated  ·  exact measured"
                        color: root.dimmed
                        font.family: root.fontFamily
                        font.pixelSize: Style.font.caption
                    }

                }

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

}
