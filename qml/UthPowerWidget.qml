// Main layout for the UTH Power panel.

import QtQuick
import QtQuick.Shapes
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// Places the power source on the left, the consumer stack on the right, and
// overlays animated cubic-Bezier cords that visually carry power between them.
// Component nodes are registered in a map as they are instantiated so the cord
// layer can bind to their live geometry without fragile child indexing.
Item {
    id: root

    required property var sourceData
    required property var componentsData
    required property var canonicalIds
    property bool dataLoaded: false
    property bool stale: false
    property bool reduceMotion: false
    property color foreground: Commons.Color.foreground
    property color dimmed: Qt.darker(foreground, 1.25)
    property color accent: Commons.Color.accent
    property color urgent: Commons.Color.urgent
    property string fontFamily: Style.font.family
    readonly property int innerSpacing: Style.space(36)
    readonly property int nodeSpacing: Style.space(14)
    // Map of component id -> ComponentNode instance. Populated by delegates so
    // the cord layer can read each node's live position and size.
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

    implicitWidth: layout.implicitWidth
    implicitHeight: layout.implicitHeight

    // Cords are drawn behind the source/component nodes so the port markers
    // inside those nodes cover the cord ends and give clean terminations.
    Shape {
        id: cordLayer

        anchors.fill: layout
        preferredRendererType: Shape.CurveRenderer

        Repeater {
            model: root.canonicalIds

            AnimatedCord {
                id: cord

                required property string modelData
                readonly property var endNode: root.nodeMap[modelData] || null
                readonly property var endData: root.componentById(modelData)

                startX: sourceNode ? sourceNode.x + sourceNode.width : 0
                startY: sourceNode ? sourceNode.y + sourceNode.height / 2 : 0
                endX: endNode && componentColumn ? componentColumn.x + endNode.x : startX
                endY: endNode && componentColumn ? componentColumn.y + endNode.y + endNode.height / 2 : startY
                drawW: endData && endData.draw_w !== undefined && endData.draw_w !== null ? endData.draw_w : null
                maxW: endData && endData.max_w !== undefined && endData.max_w !== null && endData.max_w > 0 ? endData.max_w : null
                drawKind: endData && endData.draw_kind ? String(endData.draw_kind) : "measured"
                utilizationPercent: endData && typeof endData.utilization_percent === "number" ? endData.utilization_percent : 0
                activityPercent: endData && typeof endData.activity_percent === "number" ? endData.activity_percent : 0
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

    Row {
        id: layout

        spacing: root.innerSpacing

        PowerSourceNode {
            id: sourceNode

            height: componentColumn.height
            name: sourceData && sourceData.name ? String(sourceData.name) : ""
            totalDrawW: sourceData && sourceData.total_draw_w !== undefined && sourceData.total_draw_w !== null ? sourceData.total_draw_w : null
            totalDrawKind: sourceData && sourceData.total_draw_kind ? String(sourceData.total_draw_kind) : "none"
            batteryPercentage: sourceData && typeof sourceData.battery_percentage === "number" ? sourceData.battery_percentage : -1
            dataLoaded: root.dataLoaded
            stale: root.stale
            foreground: root.foreground
            dimmed: root.dimmed
            accent: root.accent
            urgent: root.urgent
            fontFamily: root.fontFamily
        }

        Column {
            id: componentColumn

            spacing: root.nodeSpacing

            Repeater {
                model: root.canonicalIds

                ComponentNode {
                    id: componentNode

                    required property string modelData

                    nodeId: modelData
                    labelData: {
                        var c = root.componentById(modelData);
                        return c && c.label ? String(c.label) : modelData.toUpperCase();
                    }
                    drawW: {
                        var c = root.componentById(modelData);
                        return c && c.draw_w !== undefined && c.draw_w !== null ? c.draw_w : null;
                    }
                    maxW: {
                        var c = root.componentById(modelData);
                        return c && c.max_w !== undefined && c.max_w !== null && c.max_w > 0 ? c.max_w : null;
                    }
                    drawKind: {
                        var c = root.componentById(modelData);
                        return c && c.draw_kind ? String(c.draw_kind) : "measured";
                    }
                    drawSource: {
                        var c = root.componentById(modelData);
                        return c && c.draw_source ? String(c.draw_source) : "";
                    }
                    utilizationPercent: {
                        var c = root.componentById(modelData);
                        return c && typeof c.utilization_percent === "number" ? c.utilization_percent : 0;
                    }
                    uncertaintyPercent: {
                        var c = root.componentById(modelData);
                        return c && typeof c.uncertainty_percent === "number" ? c.uncertainty_percent : 0;
                    }
                    modelName: {
                        var c = root.componentById(modelData);
                        return c && c.model && typeof c.model === "object" && c.model.id !== undefined ? String(c.model.id) : (c && c.model ? String(c.model) : "");
                    }
                    activityPercent: {
                        var c = root.componentById(modelData);
                        return c && typeof c.activity_percent === "number" ? c.activity_percent : 0;
                    }
                    available: !!root.componentById(modelData)
                    dataLoaded: root.dataLoaded
                    stale: root.stale
                    reduceMotion: root.reduceMotion
                    foreground: root.foreground
                    dimmed: root.dimmed
                    accent: root.accent
                    fontFamily: root.fontFamily
                    Component.onCompleted: root.registerNode(nodeId, componentNode)
                    Component.onDestruction: root.unregisterNode(nodeId, componentNode)
                }

            }

        }

    }

}
