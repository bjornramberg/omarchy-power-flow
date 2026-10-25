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
    id: root

    // Parsed telemetry state.
    property var sourceData: ({
        "name": "",
        "total_draw_w": null,
        "total_draw_kind": "none",
        "battery_percentage": -1
    })
    property var componentsData: []
    property bool dataLoaded: false
    property bool stale: false
    property bool expectedStop: false
    readonly property bool reduceMotion: Style.reduceMotion !== undefined && Style.reduceMotion === true
    readonly property var canonicalIds: ["cpu", "gpu", "ram", "disk"]
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

    function handleLine(line) {
        if (!line || !line.length)
            return ;

        var parsed;
        try {
            parsed = JSON.parse(String(line));
        } catch (e) {
            console.warn("UTH Power: failed to parse collector line:", e);
            return ;
        }
        if (!parsed || typeof parsed !== "object")
            return ;

        stale = false;
        staleTimer.restart();
        var nextSource = parsed.source;
        if (nextSource && typeof nextSource === "object")
            sourceData = nextSource;

        componentsData = Array.isArray(parsed.components) ? parsed.components : [];
        dataLoaded = true;
    }

    moduleName: "uth.power"
    ipcTarget: "uth.power"
    onOpenedChanged: {
        if (opened) {
            stale = false;
            if (dataLoaded)
                staleTimer.restart();

            if (!collector.running)
                collector.running = true;

        } else {
            stale = false;
            staleTimer.stop();
            if (collector.running) {
                expectedStop = true;
                collector.running = false;
            }
        }
    }
    implicitWidth: button.implicitWidth
    implicitHeight: button.implicitHeight

    Process {
        id: collector

        command: [root.binaryPath]
        onExited: function(exitCode) {
            var expected = root.expectedStop;
            root.expectedStop = false;
            if (!expected && exitCode !== 0)
                console.warn("UTH Power: collector exited unexpectedly with code", exitCode);

            if (root.opened && !collector.running)
                Qt.callLater(function() {
                if (root.opened && !collector.running)
                    collector.running = true;

            });

        }

        stdout: SplitParser {
            onRead: function(line) {
                root.handleLine(line);
            }
        }

        stderr: StdioCollector {
            waitForEnd: true
            onStreamFinished: {
                var err = String(text || "").trim();
                if (err)
                    console.warn("UTH Power collector stderr:", err);

            }
        }

    }

    Timer {
        id: staleTimer

        interval: 3500
        repeat: false
        onTriggered: root.stale = true
    }

    BarIconButton {
        id: button

        anchors.fill: parent
        bar: root.bar
        text: "\uf0e7" // nf-fa-bolt
        tooltipText: "UTH Power"
        onPressed: function(b) {
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
        contentWidth: panel.fittedContentWidth(dashboard.implicitWidth + panel.padding * 2 + Border.left(panel.borderSpec) + Border.right(panel.borderSpec) + Style.space(24), Style.space(560))
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
