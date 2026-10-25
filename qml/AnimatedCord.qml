import QtQuick
import QtQuick.Shapes
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// Animated cubic-Bezier cord that visually carries power from the source to a
// single consumer. The cord renders as one ShapePath: a flowing dashed line in
// normal motion, and a clean static solid line when reduced motion is enabled
// or the consumer is unavailable/stale.
Shape {
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
    // Geometry.
    readonly property real dx: root.endX - root.startX
    readonly property real dy: root.endY - root.startY
    readonly property real cp1X: root.startX + root.dx * 0.55
    readonly property real cp1Y: root.startY + root.dy * 0.05
    readonly property real cp2X: root.endX - root.dx * 0.55
    readonly property real cp2Y: root.endY - root.dy * 0.05
    // Visual state.
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
    readonly property color strokeColor: {
        if (root.inert)
            return Util.alpha(root.dimmed, 0.22);

        var f = Math.pow(root.effectiveFraction, 0.6);
        var loA = 0.5;
        var hiA = 0.25 + 0.75 * f;
        return Qt.rgba((1 - f) * root.foreground.r + f * root.accent.r, (1 - f) * root.foreground.g + f * root.accent.g, (1 - f) * root.foreground.b + f * root.accent.b, Math.max(loA, hiA));
    }
    // Dash pattern. A fixed cycle length keeps the loop seamless regardless of
    // the actual curve length (which Qt Quick Shapes does not expose cheaply).
    readonly property real dashOn: Math.max(4, Style.spaceReal(10))
    readonly property real dashOff: Math.max(3, Style.spaceReal(6))
    readonly property real cycleLength: root.dashOn + root.dashOff
    // Flow speed in pixels per millisecond.
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

    anchors.fill: parent
    preferredRendererType: Shape.CurveRenderer

    // Continuous flow. The timer fires at a fixed cadence and advances the dash
    // offset by the current (already smoothed) flow speed. Inert or reduced
    // motion states stop the timer so the cord reads as still.
    Timer {
        interval: 16
        running: !root.reduceMotion && !root.inert && root.effectiveFraction > 0.005
        repeat: true
        onTriggered: {
            var next = root.dashOffset + root.flowSpeed * interval;
            next = next % root.cycleLength;
            if (next < 0)
                next += root.cycleLength;

            root.dashOffset = next;
        }
    }

    // Single ShapePath. Reduced motion and inert states render a solid static
    // curve; normal measured states render a dashed animated curve.
    ShapePath {
        strokeWidth: root.strokeWidth
        strokeColor: root.strokeColor
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

    Behavior on flowSpeed {
        NumberAnimation {
            duration: 800
            easing.type: Easing.OutCubic
        }

    }

}
