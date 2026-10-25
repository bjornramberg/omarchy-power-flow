import QtQuick
import qs.Commons
import qs.Ui

// Pure bar trigger that opens/toggles the UTH Power panel.
// The panel is loaded by the host as the separate "panel" entry point; this
// widget does not embed the panel and intentionally carries no open-state UI.
BarWidget {
    id: root

    moduleName: "uth.power"
    implicitWidth: button.implicitWidth
    implicitHeight: button.implicitHeight

    BarIconButton {
        id: button

        anchors.fill: parent
        bar: root.bar
        text: "\uf0e7" // nf-fa-bolt
        tooltipText: "UTH Power"
        onPressed: function(b) {
            if (root.bar && root.bar.shell && typeof root.bar.shell.toggle === "function")
                root.bar.shell.toggle("uth.power", "{}");

        }
    }

}
