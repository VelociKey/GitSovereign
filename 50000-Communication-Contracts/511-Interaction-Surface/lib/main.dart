import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'dart:math' as math;

void main() {
  runApp(
    ChangeNotifierProvider(
      create: (_) => TelemetryProvider(),
      child: const GitSovereignApp(),
    ),
  );
}

class GitSovereignApp extends StatelessWidget {
  const GitSovereignApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'GitSovereign Firehorse HUD',
      theme: ThemeData.dark().copyWith(
        scaffoldBackgroundColor: const Color(0xFF0A0A0A),
        colorScheme: const ColorScheme.dark(
          primary: Color(0xFF00FF41), // Matrix Green
          secondary: Color(0xFF00A3FF),
        ),
      ),
      home: const HUDLayout(),
    );
  }
}

class TelemetryProvider extends ChangeNotifier {
  double _bps = 0.0;
  double get bps => _bps;

  void updateBPS(double value) {
    _bps = value;
    notifyListeners();
  }
}

class HUDLayout extends StatefulWidget {
  const HUDLayout({super.key});

  @override
  State<HUDLayout> createState() => _HUDLayoutState();
}

class _HUDLayoutState extends State<HUDLayout> {
  int _selectedIndex = 0;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Row(
        children: [
          NavigationRail(
            selectedIndex: _selectedIndex,
            onDestinationSelected: (int index) {
              setState(() {
                _selectedIndex = index;
              });
            },
            labelType: NavigationRailLabelType.all,
            destinations: const [
              NavigationRailDestination(
                icon: Icon(Icons.dashboard_outlined),
                selectedIcon: Icon(Icons.dashboard),
                label: Text('Dashboard'),
              ),
              NavigationRailDestination(
                icon: Icon(Icons.file_download_outlined),
                selectedIcon: Icon(Icons.file_download),
                label: Text('Harvests'),
              ),
              NavigationRailDestination(
                icon: Icon(Icons.shield_outlined),
                selectedIcon: Icon(Icons.shield),
                label: Text('Identity'),
              ),
            ],
          ),
          const VerticalDivider(thickness: 1, width: 1),
          Expanded(
            child: _buildView(_selectedIndex),
          ),
        ],
      ),
    );
  }

  Widget _buildView(int index) {
    switch (index) {
      case 0:
        return const DashboardView();
      case 1:
        return const SovereignTreeView();
      default:
        return const Center(child: Text('Under Construction'));
    }
  }
}

class DashboardView extends StatelessWidget {
  const DashboardView({super.key});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(24.0),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'FIREHORSE TELEMETRY HEARTBEAT',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold, letterSpacing: 2),
          ),
          const SizedBox(height: 24),
          const Expanded(
            child: Center(
              child: HeartbeatVisualizer(),
            ),
          ),
          const SizedBox(height: 24),
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              _buildStatCard('EGRESS STATUS', 'STABLE', Colors.green),
              _buildStatCard('ACTIVE PIPES', '4', Colors.blue),
              _buildStatCard('LATENCY', '42ms', Colors.orange),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildStatCard(String label, String value, Color color) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        border: Border.all(color: color.withOpacity(0.5)),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        children: [
          Text(label, style: TextStyle(color: color, fontSize: 12)),
          const SizedBox(height: 8),
          Text(value, style: const TextStyle(fontSize: 20, fontWeight: FontWeight.bold)),
        ],
      ),
    );
  }
}

class SovereignTreeView extends StatelessWidget {
  const SovereignTreeView({super.key});

  @override
  Widget build(BuildContext context) {
    final repos = [
      {'name': 'OlympusForge', 'status': 'Sovereign', 'hash': 'a1b2...'},
      {'name': 'OlympusConductor', 'status': 'Syncing', 'hash': 'c3d4...'},
      {'name': 'InteractionSurface', 'status': 'Novel', 'hash': 'e5f6...'},
    ];

    return ListView.builder(
      itemCount: repos.length,
      itemBuilder: (context, index) {
        final repo = repos[index];
        return ListTile(
          leading: const Icon(Icons.folder_copy_outlined),
          title: Text(repo['name']!),
          subtitle: Text('Hash: ${repo['hash']}'),
          trailing: Chip(
            label: Text(repo['status']!),
            backgroundColor: repo['status'] == 'Sovereign' ? Colors.green.withOpacity(0.2) : Colors.blue.withOpacity(0.2),
          ),
        );
      },
    );
  }
}

class HeartbeatVisualizer extends StatefulWidget {
  const HeartbeatVisualizer({super.key});

  @override
  State<HeartbeatVisualizer> createState() => _HeartbeatVisualizerState();
}

class _HeartbeatVisualizerState extends State<HeartbeatVisualizer> with SingleTickerProviderStateMixin {
  late AnimationController _controller;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(seconds: 2),
    )..repeat();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, child) {
        return CustomPaint(
          size: const Size(double.infinity, 200),
          painter: SineWavePainter(
            progress: _controller.value,
            color: Theme.of(context).colorScheme.primary,
          ),
        );
      },
    );
  }
}

class SineWavePainter extends CustomPainter {
  final double progress;
  final Color color;

  SineWavePainter({required this.progress, required this.color});

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2.0;

    final path = Path();
    final centerY = size.height / 2;
    
    for (double x = 0; x <= size.width; x++) {
      // Sine wave formula: y = A * sin(B(x + C)) + D
      // progress makes it move
      final y = centerY + 40 * math.sin((x / size.width * 4 * math.pi) + (progress * 2 * math.pi));
      if (x == 0) {
        path.moveTo(x, y);
      } else {
        path.lineTo(x, y);
      }
    }

    canvas.drawPath(path, paint);
    
    // Add glowing pulse effect
    final glowPaint = Paint()
      ..color = color.withOpacity(0.3)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 6.0
      ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 4);
    canvas.drawPath(path, glowPaint);
  }

  @override
  bool shouldRepaint(covariant SineWavePainter oldDelegate) => true;
}
