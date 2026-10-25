import QtQuick
import QtQuick.Shapes
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// Directional energy cord from the source hub to a single consumer.
// Renders a dim continuous base path plus a brighter dashed flow and small
// travelling packets. Reduced motion collapses to a clean static line.
Item {
    id: root

    required property real startX
    required property real startY
    required property real endX
    required property real endY
    property var drawW: null
    property var maxW: null
    property string drawKind: "measured"
    property int utilizationPercent: 0
    property int activityPercent: 0
    property bool available: true
    property bool dataLoaded: false
    property bool stale: false
    property bool reduceMotion: false
    property color foreground: Commons.Color.foreground
    property color dimmed: Qt.darker(foreground, 1.25)
    property color accent: Commons.Color.accent
    // Bezier geometry.
    readonly property real dx: root.endX - root.startX
    readonly property real dy: root.endY - root.startY
    readonly property real cp1X: root.startX + root.dx * 0.55
    readonly property real cp1Y: root.startY + root.dy * 0.05
    readonly property real cp2X: root.endX - root.dx * 0.55
    readonly property real cp2Y: root.endY - root.dy * 0.05
    readonly property bool inert: !root.dataLoaded || !root.available || root.stale
    readonly property real effectiveDrawW: root.drawW !== null && root.drawW !== undefined && isFinite(Number(root.drawW)) ? Number(root.drawW) : 0
    readonly property real effectiveMaxW: root.maxW !== null && root.maxW !== undefined && Number(root.maxW) > 0 ? Number(root.maxW) : 100
    readonly property real effectiveFraction: {
        if (root.drawW !== null && root.drawW !== undefined && isFinite(Number(root.drawW)))
            return Math.max(0, Math.min(1, Number(root.drawW) / root.effectiveMaxW));

        if (root.utilizationPercent > 0)
            return root.utilizationPercent / 100;

        return root.activityPercent / 100;
    }
    readonly property real strokeWidth: {
        var baseLo = Math.max(1.2, Style.spaceReal(1.5));
        var baseHi = Math.max(baseLo, Style.spaceReal(3));
        var base = baseLo + (baseHi - baseLo) * Math.pow(root.effectiveFraction, 0.7);
        return root.inert ? Math.max(1, base * 0.65) : base;
    }
    readonly property color baseStrokeColor: Util.alpha(root.dimmed, root.inert ? 0.18 : 0.32)
    readonly property color flowStrokeColor: {
        if (root.inert)
            return Util.alpha(root.dimmed, 0.22);

        var f = Math.pow(root.effectiveFraction, 0.6);
        return Qt.rgba((1 - f) * root.foreground.r + f * root.accent.r, (1 - f) * root.foreground.g + f * root.accent.g, (1 - f) * root.foreground.b + f * root.accent.b, 0.5 + 0.45 * f);
    }
    readonly property real dashOn: Math.max(4, Style.spaceReal(10))
    readonly property real dashOff: Math.max(3, Style.spaceReal(6))
    readonly property real cycleLength: root.dashOn + root.dashOff
    readonly property real baseDuration: 2600
    readonly property real fastDuration: 320
    readonly property real targetDuration: {
        if (root.inert || root.effectiveFraction <= 0.005)
            return root.baseDuration;

        var f = Math.pow(root.effectiveFraction, 0.5);
        return root.fastDuration + (root.baseDuration - root.fastDuration) * (1 - f);
    }
    property real flowSpeed: root.cycleLength / Math.max(50, root.targetDuration)
    property real dashOffset: 0

    function bezierX(t) {
        var u = 1 - t;
        return u * u * u * root.startX + 3 * u * u * t * root.cp1X + 3 * u * t * t * root.cp2X + t * t * t * root.endX;
    }

    function bezierY(t) {
        var u = 1 - t;
        return u * u * u * root.startY + 3 * u * u * t * root.cp1Y + 3 * u * t * t * root.cp2Y + t * t * t * root.endY;
    }

    anchors.fill: parent

    // Dim continuous base path — always visible so direction reads even when
    // the flow is subtle or motion is reduced.
    Shape {
        anchors.fill: parent
        preferredRendererType: Shape.CurveRenderer

        ShapePath {
            strokeWidth: root.strokeWidth * 0.65
            strokeColor: root.baseStrokeColor
            fillColor: "transparent"
            capStyle: ShapePath.RoundCap
            strokeStyle: ShapePath.SolidLine
            startX: root.startX
            startY: root.startY

            PathCubic {
                control1X: root.cp1X
                control1Y: root.cp1Y
                control2X: root.cp2X
                control2Y: root.cp2Y
                x: root.endX
                y: root.endY
            }

        }

    }

    // Brighter dashed flow. The offset runs in reverse so the dashes visibly
    // travel from the source start point toward the component end point.
    Shape {
        anchors.fill: parent
        preferredRendererType: Shape.CurveRenderer

        ShapePath {
            strokeWidth: root.strokeWidth
            strokeColor: root.flowStrokeColor
            fillColor: "transparent"
            capStyle: ShapePath.RoundCap
            strokeStyle: root.reduceMotion || root.inert ? ShapePath.SolidLine : ShapePath.DashLine
            dashPattern: [root.dashOn, root.dashOff]
            dashOffset: root.dashOffset
            startX: root.startX
            startY: root.startY

            PathCubic {
                control1X: root.cp1X
                control1Y: root.cp1Y
                control2X: root.cp2X
                control2Y: root.cp2Y
                x: root.endX
                y: root.endY
            }

        }

    }

    // Travelling energy packets. Three small circles spaced evenly along the
    // curve; they wrap around to keep the flow continuous.
    Item {
        anchors.fill: parent
        visible: !root.reduceMotion && !root.inert && root.effectiveFraction > 0.005

        Repeater {
            model: 3

            Rectangle {
                required property int index
                property real progress: index / 3

                width: Math.max(4, Style.spaceReal(3))
                height: width
                radius: width / 2
                color: root.accent
                opacity: 0.9 - index * 0.22
                x: root.bezierX(progress) - width / 2
                y: root.bezierY(progress) - height / 2

                Timer {
                    interval: 16
                    running: parent.visible
                    repeat: true
                    onTriggered: {
                        var next = parent.progress + interval / root.targetDuration;
                        parent.progress = next % 1;
                    }
                }

            }

        }

    }

    Timer {
        interval: 16
        running: !root.reduceMotion && !root.inert && root.effectiveFraction > 0.005
        repeat: true
        onTriggered: {
            var next = root.dashOffset - root.flowSpeed * interval;
            next = next % root.cycleLength;
            if (next < 0)
                next += root.cycleLength;

            root.dashOffset = next;
        }
    }

    Behavior on flowSpeed {
        NumberAnimation {
            duration: 800
            easing.type: Easing.OutCubic
        }

    }

}
