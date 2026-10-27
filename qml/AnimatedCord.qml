import QtQuick
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// Directional energy cord from the source hub to a single consumer.
// A single Canvas draws the continuous cubic Bezier gradient stroke;
// thickness and end colour scale with the component's share of the
// visible total. Travelling packets provide source-to-component motion.
Item {
    id: root

    required property real startX
    required property real startY
    required property real endX
    required property real endY
    property var drawW: null
    property var maxW: null
    property var sourceTotalW: null
    property real maxShare: 0
    property bool isDominant: false
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
    // Share of the source's visible total watts. This stays a true total-share
    // value internally; the visual normalization below maps it against the
    // largest visible component so the dominant branch reads as "full" while
    // still preserving the actual share semantics.
    readonly property real sourceTotal: root.sourceTotalW !== null && root.sourceTotalW !== undefined && isFinite(Number(root.sourceTotalW)) ? Number(root.sourceTotalW) : 0
    readonly property real shareFraction: root.sourceTotal > 0 ? Math.max(0, Math.min(1, root.effectiveDrawW / root.sourceTotal)) : root.effectiveFraction
    // Visual normalization: dominant = 1.0, others scaled relative to it.
    // A small floor keeps low-share branches visible as continuous dim tracks.
    readonly property real visualFraction: root.maxShare > 0 ? Math.max(0.04, Math.min(1, root.shareFraction / root.maxShare)) : Math.max(0.04, Math.min(1, root.shareFraction))
    // Visual weight keyed off the normalized share so the dominant branch is
    // clearly thicker than the runner-up, while tiny branches stay legible.
    readonly property real strokeWidth: {
        var baseLo = Math.max(1.2, Style.spaceReal(1.5));
        var baseHi = Math.max(baseLo, Style.spaceReal(4.25));
        var base = baseLo + (baseHi - baseLo) * root.visualFraction;
        return root.inert ? Math.max(1, base * 0.65) : base;
    }
    readonly property real baseDuration: 2600
    readonly property real fastDuration: 280
    readonly property real targetDuration: {
        if (root.inert || root.visualFraction <= 0.005)
            return root.baseDuration;

        var f = Math.pow(root.visualFraction, 0.45);
        return root.fastDuration + (root.baseDuration - root.fastDuration) * (1 - f);
    }

    function bezierX(t) {
        var u = 1 - t;
        return u * u * u * root.startX + 3 * u * u * t * root.cp1X + 3 * u * t * t * root.cp2X + t * t * t * root.endX;
    }

    function bezierY(t) {
        var u = 1 - t;
        return u * u * u * root.startY + 3 * u * u * t * root.cp1Y + 3 * u * t * t * root.cp2Y + t * t * t * root.endY;
    }

    function gradientEndColor() {
        var vf = Math.max(0, Math.min(1, root.visualFraction));
        var hueMix = Math.pow(vf, 0.7);
        var r = (1 - hueMix) * root.foreground.r + hueMix * root.accent.r;
        var g = (1 - hueMix) * root.foreground.g + hueMix * root.accent.g;
        var b = (1 - hueMix) * root.foreground.b + hueMix * root.accent.b;
        var a = 0.22 + 0.7 * vf;
        if (root.inert)
            a *= 0.5;

        return Qt.rgba(r, g, b, a);
    }

    function cssColor(c) {
        return "rgba(" + Math.round(c.r * 255) + "," + Math.round(c.g * 255) + "," + Math.round(c.b * 255) + "," + c.a + ")";
    }

    function requestGradientPaint() {
        if (gradientCanvas)
            gradientCanvas.requestPaint();

    }

    anchors.fill: parent
    onStartXChanged: root.requestGradientPaint()
    onStartYChanged: root.requestGradientPaint()
    onEndXChanged: root.requestGradientPaint()
    onEndYChanged: root.requestGradientPaint()
    onVisualFractionChanged: root.requestGradientPaint()
    onStrokeWidthChanged: root.requestGradientPaint()
    onInertChanged: root.requestGradientPaint()
    onForegroundChanged: root.requestGradientPaint()
    onDimmedChanged: root.requestGradientPaint()
    onAccentChanged: root.requestGradientPaint()
    onIsDominantChanged: root.requestGradientPaint()
    Component.onCompleted: root.requestGradientPaint()

    // Continuous cubic gradient track and restrained dominant halo.
    // Repainted only when geometry or intensity changes, not per frame.
    Canvas {
        id: gradientCanvas

        anchors.fill: parent
        enabled: false
        onWidthChanged: requestPaint()
        onHeightChanged: requestPaint()
        onPaint: {
            var ctx = getContext("2d");
            ctx.clearRect(0, 0, width, height);
            // Soft halo behind the dominant branch.
            if (root.isDominant && !root.inert && root.visualFraction > 0.15) {
                ctx.save();
                ctx.beginPath();
                ctx.moveTo(root.startX, root.startY);
                ctx.bezierCurveTo(root.cp1X, root.cp1Y, root.cp2X, root.cp2Y, root.endX, root.endY);
                ctx.lineWidth = root.strokeWidth * 1.9;
                ctx.lineCap = "round";
                ctx.strokeStyle = root.cssColor(Util.alpha(root.accent, 0.1 + 0.1 * root.visualFraction));
                ctx.globalAlpha = 0.5;
                ctx.stroke();
                ctx.restore();
            }
            // Primary continuous gradient cord.
            ctx.save();
            ctx.beginPath();
            ctx.moveTo(root.startX, root.startY);
            ctx.bezierCurveTo(root.cp1X, root.cp1Y, root.cp2X, root.cp2Y, root.endX, root.endY);
            ctx.lineWidth = root.strokeWidth;
            ctx.lineCap = "round";
            var grad = ctx.createLinearGradient(root.startX, root.startY, root.endX, root.endY);
            var startColor = Util.alpha(root.dimmed, root.inert ? 0.09 : 0.18);
            var endColor = root.gradientEndColor();
            grad.addColorStop(0, root.cssColor(startColor));
            grad.addColorStop(1, root.cssColor(endColor));
            ctx.strokeStyle = grad;
            ctx.globalAlpha = 1;
            ctx.stroke();
            ctx.restore();
        }
        onVisibleChanged: {
            if (visible)
                requestPaint();

        }
    }

    // Travelling energy packets. Three circles spaced evenly along the curve;
    // they scale and brighten with the normalized share.
    Item {
        anchors.fill: parent
        visible: !root.reduceMotion && !root.inert && root.visualFraction > 0.005

        Repeater {
            model: 3

            Rectangle {
                required property int index
                readonly property real packetFraction: Math.max(0, Math.min(1, root.visualFraction))
                readonly property real packetSize: Math.max(3, Style.spaceReal(3) + Style.spaceReal(4) * Math.pow(packetFraction, 0.5))
                property real progress: index / 3

                width: packetSize
                height: packetSize
                radius: width / 2
                color: root.accent
                opacity: 0.95 - index * 0.25 + 0.15 * packetFraction
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

}
