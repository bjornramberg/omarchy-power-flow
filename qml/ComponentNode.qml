import QtQuick
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// Right-hand consumer node: label, current draw/load, and a small activity bar.
// Renders a muted unavailable state when the collector did not emit this id.
Item {
    id: root

    property string nodeId: ""
    property string labelData: ""
    property var drawW: null
    property var maxW: null
    property string drawKind: "measured"
    property string drawSource: ""
    property int utilizationPercent: 0
    property int uncertaintyPercent: 0
    property string modelName: ""
    property int activityPercent: 0
    property bool available: true
    property bool dataLoaded: false
    property bool stale: false
    property bool reduceMotion: false
    property color foreground: Commons.Color.foreground
    property color dimmed: Qt.darker(foreground, 1.25)
    property color accent: Commons.Color.accent
    property string fontFamily: Style.font.family
    // Smoothed display values.
    property real displayDrawW: 0
    readonly property real effectiveDrawW: root.drawW !== null && root.drawW !== undefined && isFinite(Number(root.drawW)) ? Number(root.drawW) : 0
    readonly property real effectiveMaxW: root.maxW !== null && root.maxW !== undefined && Number(root.maxW) > 0 ? Number(root.maxW) : 100
    // Fraction shown by the activity bar: match the adjacent label metric
    // (CPU/RAM load, GPU/DISK activity) rather than raw wattage.
    readonly property real effectiveFraction: {
        if (root.nodeId === "cpu" || root.nodeId === "ram")
            return Math.max(0, Math.min(1, root.utilizationPercent / 100));

        return Math.max(0, Math.min(1, root.activityPercent / 100));
    }
    readonly property bool showUnavailable: !root.dataLoaded || !root.available || root.stale
    readonly property bool hasWatts: root.drawW !== null && root.drawW !== undefined && isFinite(Number(root.drawW))

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

        if (!root.available)
            return "unavailable";

        if (root.nodeId === "cpu")
            return root.utilizationPercent + "% load";

        if (root.nodeId === "ram")
            return root.utilizationPercent + "% used";

        return root.activityPercent + "%";
    }

    function tooltipText() {
        var parts = [];
        if (root.drawSource)
            parts.push("Source: " + root.drawSource);

        if (root.modelName)
            parts.push("Model: " + root.modelName);

        if (root.uncertaintyPercent > 0)
            parts.push("Uncertainty: ±" + root.uncertaintyPercent + "%");

        return parts.join("\n");
    }

    onEffectiveDrawWChanged: displayDrawW = root.effectiveDrawW
    implicitWidth: row.implicitWidth
    implicitHeight: row.implicitHeight

    Row {
        id: row

        spacing: Style.space(14)
        anchors.verticalCenter: parent.verticalCenter

        Column {
            id: labelColumn

            spacing: Style.space(2)
            width: Math.max(Style.space(46), implicitWidth)

            Text {
                textFormat: Text.PlainText
                text: root.labelData
                color: root.showUnavailable ? root.dimmed : root.foreground
                font.family: root.fontFamily
                font.pixelSize: Style.font.title
                font.bold: true
                opacity: root.showUnavailable ? 0.65 : 1
            }

            Text {
                textFormat: Text.PlainText
                text: root.showUnavailable ? "—" : formatWatts(root.displayDrawW, root.drawKind)
                color: root.showUnavailable ? root.dimmed : (root.hasWatts && root.effectiveDrawW > 0 ? root.accent : root.foreground)
                font.family: root.fontFamily
                font.pixelSize: Style.font.body
                font.bold: true
                opacity: root.showUnavailable ? 0.55 : (root.hasWatts && root.effectiveDrawW > 0 ? 1 : 0.75)
            }

        }

        Item {
            readonly property int barWidth: Style.space(72)
            readonly property int barHeight: Math.max(Style.space(4), Style.spacing.sm)

            width: barWidth
            height: labelColumn.height

            Rectangle {
                id: track

                anchors.bottom: parent.bottom
                anchors.bottomMargin: Style.space(2)
                width: parent.barWidth
                height: parent.barHeight
                radius: height / 2
                color: Util.alpha(root.foreground, 0.18)
            }

            Rectangle {
                id: fill

                anchors.left: track.left
                anchors.verticalCenter: track.verticalCenter
                height: track.height
                radius: track.radius
                width: Math.max(track.height, track.width * root.effectiveFraction)
                color: root.showUnavailable ? Util.alpha(root.foreground, 0.22) : (root.effectiveFraction > 0 ? root.accent : Util.alpha(root.foreground, 0.4))
                opacity: root.showUnavailable ? 0.55 : 1

                Behavior on width {
                    NumberAnimation {
                        duration: root.reduceMotion ? 50 : 600
                        easing.type: Easing.OutCubic
                    }

                }

                Behavior on color {
                    ColorAnimation {
                        duration: 220
                    }

                }

            }

            Text {
                anchors.top: track.bottom
                anchors.topMargin: Style.space(2)
                anchors.right: track.right
                textFormat: Text.PlainText
                text: root.loadText()
                color: root.dimmed
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                opacity: root.showUnavailable ? 0.55 : 0.85
            }

        }

    }

    // Input port where the cord terminates. Centered on the left edge so the
    // cord end is hidden underneath.
    Rectangle {
        id: componentPort

        anchors.verticalCenter: parent.verticalCenter
        x: -width / 2
        width: Style.space(10)
        height: width
        radius: width / 2
        color: root.showUnavailable ? root.dimmed : root.accent
        // Keep available ports nearly opaque so they cleanly cover the cord end.
        opacity: root.showUnavailable ? 0.25 : (0.9 + 0.05 * root.effectiveFraction)
        border.color: root.foreground
        border.width: root.showUnavailable ? 0 : Math.max(1, Style.spaceReal(1))
    }

    // Tooltip with backend-provided provenance details when available.
    MouseArea {
        anchors.fill: parent
        hoverEnabled: true
        acceptedButtons: Qt.NoButton

        PanelToolTip {
            visible: parent.containsMouse && root.tooltipText() !== ""
            text: root.tooltipText()
            fontFamily: root.fontFamily
        }

    }

    Behavior on displayDrawW {
        NumberAnimation {
            duration: 700
            easing.type: Easing.OutCubic
        }

    }

}
