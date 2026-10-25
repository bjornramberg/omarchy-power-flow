// UTH Power panel plugin entry point.

import QtQuick
import Quickshell
import Quickshell.Io
import Quickshell.Wayland
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// This plugin provides both a bar widget (qml/BarWidget.qml) and this panel.
// The bar icon toggles the panel, which is summoned and dismissed by the
// Omarchy shell. The panel launches the bundled Go collector, parses one JSON
// object per line, and visualises power flowing from the source
// (AC/battery/unknown) to CPU/GPU/RAM/DISK consumers.
Item {
    id: root

    // Host-provided facade for third-party plugins.
    property var shell: null
    property var manifest: null
    // Logical open state set by the host.
    property bool opened: false
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
    property bool reduceMotion: Style.reduceMotion !== undefined && Style.reduceMotion === true
    // Canonical consumers we always show, even when the collector omits them.
    readonly property var canonicalIds: ["cpu", "gpu", "ram", "disk"]
    // The public third-party manifest keeps __sourceDir so plugins can locate
    // bundled helpers. Fall back to resolving relative to this QML file for
    // standalone test runs.
    readonly property string binaryPath: {
        var dir = root.manifest && root.manifest.__sourceDir ? String(root.manifest.__sourceDir) : "";
        if (dir)
            return dir + "/bin/uth-power";

        var url = String(Qt.resolvedUrl("../bin/uth-power") || "");
        if (url.indexOf("file://") === 0)
            url = url.substring(7);

        try {
            url = decodeURIComponent(url);
        } catch (e) {
        }
        return url;
    }
    property bool expectedStop: false

    function open(payloadJson) {
        opened = true;
    }

    function close() {
        opened = false;
    }

    function dismiss() {
        if (shell && typeof shell.hide === "function")
            shell.hide((manifest && manifest.id) || "uth.power");
        else
            root.close();
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

        var nextComponents = Array.isArray(parsed.components) ? parsed.components : [];
        componentsData = nextComponents;
        dataLoaded = true;
    }

    onOpenedChanged: {
        if (opened) {
            stale = false;
            if (root.dataLoaded)
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

    Process {
        id: collector

        command: [root.binaryPath]
        onExited: function(exitCode) {
            var expected = root.expectedStop;
            root.expectedStop = false;
            if (!expected && exitCode !== 0)
                console.warn("UTH Power: collector exited unexpectedly with code", exitCode);

            if (root.opened)
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

    PanelWindow {
        id: window

        visible: root.opened
        color: "transparent"
        exclusionMode: ExclusionMode.Ignore
        WlrLayershell.namespace: "uth-power"
        WlrLayershell.layer: WlrLayer.Overlay
        WlrLayershell.keyboardFocus: WlrKeyboardFocus.Exclusive
        onVisibleChanged: {
            if (visible)
                Qt.callLater(function() {
                keyCatcher.forceActiveFocus();
            });

        }

        anchors {
            top: true
            bottom: true
            left: true
            right: true
        }

        // Dim scrim that dismisses on click outside the card.
        Rectangle {
            anchors.fill: parent
            color: Commons.Color.menu.scrim

            MouseArea {
                anchors.fill: parent
                onClicked: root.dismiss()
            }

        }

        Item {
            id: keyCatcher

            anchors.fill: parent
            focus: true
            Keys.onEscapePressed: root.dismiss()

            // Centered content cluster, scaled down on small outputs instead of clipping.
            Item {
                id: cluster

                anchors.centerIn: parent
                width: card.width
                height: card.height
                scale: Math.min(1, (keyCatcher.width - Style.space(32)) / Math.max(1, width), (keyCatcher.height - Style.space(32)) / Math.max(1, height))

                BorderSurface {
                    id: card

                    width: widget.implicitWidth + card.contentLeftInset + card.contentRightInset
                    height: widget.implicitHeight + card.contentTopInset + card.contentBottomInset
                    color: Commons.Color.popups.background
                    borderSpec: Border.surfaceSpec("popups", "border", Commons.Color.popups.border, Math.max(1, Style.space(2)))
                    radius: Style.cornerRadius
                    padding: Style.spacing.panelPadding

                    UthPowerWidget {
                        id: widget

                        anchors.fill: parent
                        anchors.topMargin: card.contentTopInset
                        anchors.rightMargin: card.contentRightInset
                        anchors.bottomMargin: card.contentBottomInset
                        anchors.leftMargin: card.contentLeftInset
                        sourceData: root.sourceData
                        componentsData: root.componentsData
                        canonicalIds: root.canonicalIds
                        dataLoaded: root.dataLoaded
                        stale: root.stale
                        reduceMotion: root.reduceMotion
                        foreground: Commons.Color.popups.text
                        dimmed: Qt.darker(Commons.Color.popups.text, 1.25)
                        accent: Commons.Color.accent
                        urgent: Commons.Color.urgent
                        fontFamily: Style.font.family
                    }

                    // Swallow clicks on the card so they do not dismiss the panel.
                    MouseArea {
                        anchors.fill: parent
                        onClicked: {
                        }
                    }

                }

            }

        }

    }

}
