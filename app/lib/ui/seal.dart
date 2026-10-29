import 'package:flutter/material.dart';

class OtakaseSeal extends StatelessWidget {
  const OtakaseSeal({super.key, this.size = 34});
  final double size;
  @override
  Widget build(BuildContext context) => ExcludeSemantics(
    child: SizedBox(
      width: size,
      height: size,
      child: CustomPaint(painter: _SealPainter()),
    ),
  );
}

class _SealPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    canvas.scale(size.width / 100, size.height / 100);
    canvas.drawPath(
      Path()
        ..moveTo(37.79, 54.346000000000004)
        ..lineTo(74.45, 54.346000000000004)
        ..lineTo(80.3, 46.234)
        ..quadraticBezierTo(80.3, 46.234, 81.35300000000001, 47.131)
        ..quadraticBezierTo(82.406, 48.028, 84.04400000000001, 49.432)
        ..quadraticBezierTo(85.682, 50.836, 87.476, 52.396)
        ..quadraticBezierTo(89.27, 53.956, 90.752, 55.282)
        ..quadraticBezierTo(90.596, 55.906, 89.97200000000001, 56.218)
        ..quadraticBezierTo(89.348, 56.53, 88.49, 56.53)
        ..lineTo(38.414, 56.53)
        ..close()
        ..moveTo(39.194, 86.482)
        ..lineTo(72.188, 86.482)
        ..lineTo(78.194, 78.214)
        ..quadraticBezierTo(78.194, 78.214, 79.286, 79.11099999999999)
        ..quadraticBezierTo(80.378, 80.008, 82.055, 81.451)
        ..quadraticBezierTo(83.732, 82.894, 85.526, 84.45400000000001)
        ..quadraticBezierTo(87.32, 86.014, 88.802, 87.418)
        ..quadraticBezierTo(88.724, 88.042, 88.061, 88.354)
        ..quadraticBezierTo(87.398, 88.666, 86.54, 88.666)
        ..lineTo(39.818, 88.666)
        ..close()
        ..moveTo(74.21600000000001, 19.480000000000004)
        ..lineTo(86.384, 30.634)
        ..quadraticBezierTo(
          85.604,
          31.258000000000003,
          84.122,
          31.336000000000002,
        )
        ..quadraticBezierTo(82.64, 31.414, 80.378, 30.634)
        ..quadraticBezierTo(
          75.30799999999999,
          31.804000000000002,
          68.52199999999999,
          32.74,
        )
        ..quadraticBezierTo(61.736, 33.676, 54.32599999999999, 34.222)
        ..quadraticBezierTo(46.916, 34.768, 39.896, 34.924)
        ..lineTo(39.739999999999995, 34.144)
        ..quadraticBezierTo(
          44.263999999999996,
          32.818,
          49.138999999999996,
          31.063,
        )
        ..quadraticBezierTo(54.014, 29.308, 58.733000000000004, 27.28)
        ..quadraticBezierTo(63.452, 25.252000000000002, 67.43, 23.263)
        ..quadraticBezierTo(
          71.408,
          21.274,
          74.21600000000001,
          19.480000000000004,
        )
        ..close()
        ..moveTo(24.608, 44.128)
        ..lineTo(28.741999999999997, 38.668)
        ..lineTo(38.336, 42.178)
        ..quadraticBezierTo(38.18, 42.724, 37.634, 43.114)
        ..quadraticBezierTo(37.088, 43.504, 35.918, 43.738)
        ..lineTo(35.918, 89.212)
        ..quadraticBezierTo(35.84, 89.602, 34.475, 90.382)
        ..quadraticBezierTo(33.11, 91.162, 31.043, 91.786)
        ..quadraticBezierTo(28.976, 92.41, 26.714, 92.41)
        ..lineTo(24.608, 92.41)
        ..close()
        ..moveTo(29.990000000000002, 18.232)
        ..lineTo(44.966, 22.677999999999997)
        ..quadraticBezierTo(
          44.653999999999996,
          23.458,
          43.873999999999995,
          23.926,
        )
        ..quadraticBezierTo(43.094, 24.394, 41.768, 24.316000000000003)
        ..quadraticBezierTo(38.726, 32.35, 34.787, 38.863)
        ..quadraticBezierTo(30.848, 45.376, 26.128999999999998, 50.524)
        ..quadraticBezierTo(21.41, 55.672, 15.794, 59.65)
        ..lineTo(14.936, 59.104)
        ..quadraticBezierTo(18.056, 54.111999999999995, 20.981, 47.482)
        ..quadraticBezierTo(23.906, 40.852, 26.285, 33.325)
        ..quadraticBezierTo(
          28.664,
          25.798000000000002,
          29.990000000000002,
          18.232,
        )
        ..close()
        ..moveTo(55.886, 29.698)
        ..lineTo(67.586, 29.698)
        ..lineTo(67.586, 87.496)
        ..quadraticBezierTo(67.50800000000001, 87.496, 65.09, 87.496)
        ..quadraticBezierTo(62.672, 87.496, 58.07, 87.496)
        ..lineTo(55.886, 87.496)
        ..close(),
      Paint()..color = const Color(0xff582419),
    );
    canvas.drawPath(
      Path()
        ..moveTo(34.79, 51.346000000000004)
        ..lineTo(71.45, 51.346000000000004)
        ..lineTo(77.3, 43.234)
        ..quadraticBezierTo(77.3, 43.234, 78.35300000000001, 44.131)
        ..quadraticBezierTo(79.406, 45.028, 81.04400000000001, 46.432)
        ..quadraticBezierTo(82.682, 47.836, 84.476, 49.396)
        ..quadraticBezierTo(86.27, 50.956, 87.752, 52.282)
        ..quadraticBezierTo(87.596, 52.906, 86.97200000000001, 53.218)
        ..quadraticBezierTo(86.348, 53.53, 85.49, 53.53)
        ..lineTo(35.414, 53.53)
        ..close()
        ..moveTo(36.194, 83.482)
        ..lineTo(69.188, 83.482)
        ..lineTo(75.194, 75.214)
        ..quadraticBezierTo(75.194, 75.214, 76.286, 76.11099999999999)
        ..quadraticBezierTo(77.378, 77.008, 79.055, 78.451)
        ..quadraticBezierTo(80.732, 79.894, 82.526, 81.45400000000001)
        ..quadraticBezierTo(84.32, 83.014, 85.802, 84.418)
        ..quadraticBezierTo(85.724, 85.042, 85.061, 85.354)
        ..quadraticBezierTo(84.398, 85.666, 83.54, 85.666)
        ..lineTo(36.818, 85.666)
        ..close()
        ..moveTo(71.21600000000001, 16.480000000000004)
        ..lineTo(83.384, 27.634)
        ..quadraticBezierTo(
          82.604,
          28.258000000000003,
          81.122,
          28.336000000000002,
        )
        ..quadraticBezierTo(79.64, 28.414, 77.378, 27.634)
        ..quadraticBezierTo(
          72.30799999999999,
          28.804000000000002,
          65.52199999999999,
          29.740000000000002,
        )
        ..quadraticBezierTo(
          58.736,
          30.676000000000002,
          51.32599999999999,
          31.222,
        )
        ..quadraticBezierTo(43.916, 31.768, 36.896, 31.924)
        ..lineTo(36.739999999999995, 31.144)
        ..quadraticBezierTo(
          41.263999999999996,
          29.817999999999998,
          46.138999999999996,
          28.063,
        )
        ..quadraticBezierTo(51.014, 26.308, 55.733000000000004, 24.28)
        ..quadraticBezierTo(60.452, 22.252000000000002, 64.43, 20.263)
        ..quadraticBezierTo(
          68.408,
          18.274,
          71.21600000000001,
          16.480000000000004,
        )
        ..close()
        ..moveTo(21.608, 41.128)
        ..lineTo(25.741999999999997, 35.668)
        ..lineTo(35.336, 39.178)
        ..quadraticBezierTo(35.18, 39.724, 34.634, 40.114)
        ..quadraticBezierTo(34.088, 40.504, 32.918, 40.738)
        ..lineTo(32.918, 86.212)
        ..quadraticBezierTo(32.84, 86.602, 31.475, 87.382)
        ..quadraticBezierTo(30.11, 88.162, 28.043, 88.786)
        ..quadraticBezierTo(25.976, 89.41, 23.714, 89.41)
        ..lineTo(21.608, 89.41)
        ..close()
        ..moveTo(26.990000000000002, 15.232)
        ..lineTo(41.966, 19.677999999999997)
        ..quadraticBezierTo(
          41.653999999999996,
          20.458,
          40.873999999999995,
          20.926,
        )
        ..quadraticBezierTo(40.094, 21.394, 38.768, 21.316000000000003)
        ..quadraticBezierTo(35.726, 29.35, 31.787, 35.863)
        ..quadraticBezierTo(27.848, 42.376, 23.128999999999998, 47.524)
        ..quadraticBezierTo(18.41, 52.672, 12.794, 56.65)
        ..lineTo(11.936, 56.104)
        ..quadraticBezierTo(
          15.056000000000001,
          51.111999999999995,
          17.981,
          44.482,
        )
        ..quadraticBezierTo(20.906, 37.852, 23.285, 30.325)
        ..quadraticBezierTo(
          25.664,
          22.798000000000002,
          26.990000000000002,
          15.232,
        )
        ..close()
        ..moveTo(52.886, 26.698)
        ..lineTo(64.586, 26.698)
        ..lineTo(64.586, 84.496)
        ..quadraticBezierTo(64.50800000000001, 84.496, 62.09, 84.496)
        ..quadraticBezierTo(59.672, 84.496, 55.07, 84.496)
        ..lineTo(52.886, 84.496)
        ..close(),
      Paint()..color = const Color(0xffd2492f),
    );
  }

  @override
  bool shouldRepaint(_SealPainter old) => false;
}
