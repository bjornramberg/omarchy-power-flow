import QtQuick
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// Left-hand source node: shows the current power source label, total draw,
// and battery percentage when one is available.
Item {
    id: root

    property string name: ""
    property var totalDrawW: null
    property string totalDrawKind: "none"
    property int batteryPercentage: -1
    property bool dataLoaded: false
    property bool stale: false
    property color foreground: Commons.Color.foreground
    property color dimmed: Qt.darker(foreground, 1.25)
    property color accent: Commons.Color.accent
    property color urgent: Commons.Color.urgent
    property string fontFamily: Style.font.family
    // Smoothed display value so wattage changes glide rather than jump each second.
    property real displayDrawW: 0

    function formatWatts(w) {
        var n = Number(w);
        if (!isFinite(n))
            return "—";

        if (n < 10)
            return n.toLocaleString(Qt.locale(), "f", 1) + " W";

        return Math.round(n).toLocaleString(Qt.locale(), "f", 0) + " W";
    }

    function formatSourceTotal(loaded, kind, w) {
        if (!loaded || kind === "none")
            return "—";

        var prefix = (kind === "modeled" || kind === "mixed") ? "~" : "";
        var n = Number(w);
        if (!isFinite(n))
            return prefix + "—";

        if (n < 10)
            return prefix + n.toLocaleString(Qt.locale(), "f", 1) + " W";

        return prefix + Math.round(n).toLocaleString(Qt.locale(), "f", 0) + " W";
    }

    onTotalDrawWChanged: displayDrawW = (root.totalDrawW !== null && isFinite(Number(root.totalDrawW))) ? Number(root.totalDrawW) : 0
    implicitWidth: Math.max(Style.space(110), column.implicitWidth)
    implicitHeight: column.implicitHeight

    Column {
        id: column

        anchors.verticalCenter: parent.verticalCenter
        spacing: Style.space(6)

        Text {
            textFormat: Text.PlainText
            text: (root.name && root.name.length > 0) ? root.name : "POWER"
            color: root.stale ? root.dimmed : root.foreground
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
            font.bold: true
            font.letterSpacing: 1.2
            opacity: root.stale ? 0.65 : 1
        }

        Text {
            textFormat: Text.PlainText
            text: !root.dataLoaded ? "—" : (root.stale ? "—" : formatSourceTotal(root.dataLoaded, root.totalDrawKind, root.displayDrawW))
            color: !root.dataLoaded ? root.dimmed : (root.stale ? root.dimmed : root.accent)
            font.family: root.fontFamily
            font.pixelSize: Style.font.display
            font.bold: true
            opacity: !root.dataLoaded ? 0.45 : (root.stale ? 0.55 : 1)

            Behavior on color {
                ColorAnimation {
                    duration: 220
                }

            }

        }

        Text {
            textFormat: Text.PlainText
            visible: !root.stale && root.batteryPercentage >= 0
            text: root.batteryPercentage + "%"
            color: root.dimmed
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
            font.bold: true
        }

        Text {
            textFormat: Text.PlainText
            visible: root.stale
            text: "STALE"
            color: root.urgent || Commons.Color.urgent
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
            font.bold: true
        }

    }

    // Visible output port where all cords originate. Centered on the right edge
    // so the cords start under it and the rounded caps are hidden.
    Rectangle {
        id: sourcePort

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

    Behavior on displayDrawW {
        NumberAnimation {
            duration: 700
            easing.type: Easing.OutCubic
        }

    }

}
