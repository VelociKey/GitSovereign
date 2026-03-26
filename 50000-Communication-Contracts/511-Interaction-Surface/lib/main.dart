import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'dart:math' as math;
// WASM-GC Compliance
import 'dart:js_interop'; 
import 'package:web/web.dart' as web;
import 'theme/maturity_tier.dart';

void main() {
  // Read query parameter 'tier' for deterministic graduation capture
  final search = web.window.location.search;
  final params = Uri.parse(search).queryParameters;
  final tierParam = params['tier']?.toLowerCase();
  
  MaturityTier initialTier = MaturityTier.solo;
  if (tierParam == 'team') initialTier = MaturityTier.team;
  if (tierParam == 'seed') initialTier = MaturityTier.seed;
  if (tierParam == 'beyond') initialTier = MaturityTier.beyond;
  if (tierParam == 'whitelabel') initialTier = MaturityTier.whiteLabel;

  runApp(
    MultiProvider(
      providers: [
        ChangeNotifierProvider(create: (_) => TelemetryProvider()),
        ChangeNotifierProvider(create: (_) => ThemeProvider(initialTier: initialTier)),
      ],
      child: const GitSovereignApp(),
    ),
  );
}

class ThemeProvider extends ChangeNotifier {
  MaturityTier _tier;
  ThemeProvider({MaturityTier initialTier = MaturityTier.solo}) : _tier = initialTier;
  
  MaturityTier get tier => _tier;

  void updateTier(MaturityTier newTier) {
    _tier = newTier;
    notifyListeners();
  }

  ThemeData get themeData {
    final palette = MaturityPalette.registry[_tier]!;
    return ThemeData(
      useMaterial3: true,
      colorScheme: ColorScheme.fromSeed(
        seedColor: palette.primarySeed,
        primary: palette.primarySeed,
        secondary: palette.secondarySeed,
        tertiary: palette.tertiarySeed,
        brightness: Brightness.dark,
      ),
      scaffoldBackgroundColor: const Color(0xFF0A0A0A),
      navigationRailTheme: const NavigationRailThemeData(
        backgroundColor: Color(0xFF0A0A0A),
        indicatorColor: Colors.transparent,
        selectedIconTheme: IconThemeData(color: null), 
        unselectedIconTheme: IconThemeData(color: Colors.grey),
      ),
    );
  }
}

class GitSovereignApp extends StatelessWidget {
  const GitSovereignApp({super.key});

  @override
  Widget build(BuildContext context) {
    final themeProvider = Provider.of<ThemeProvider>(context);
    return MaterialApp(
      title: 'GitSovereign Firehorse HUD',
      debugShowCheckedModeBanner: false,
      theme: themeProvider.themeData,
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
    final colorScheme = Theme.of(context).colorScheme;

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
            selectedIconTheme: IconThemeData(color: colorScheme.primary),
            unselectedIconTheme: const IconThemeData(color: Colors.white54),
            selectedLabelTextStyle: TextStyle(color: colorScheme.primary, fontWeight: FontWeight.bold),
            unselectedLabelTextStyle: const TextStyle(color: Colors.white54),
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
          VerticalDivider(thickness: 1, width: 1, color: colorScheme.outlineVariant),    
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
    final themeProvider = Provider.of<ThemeProvider>(context);
    final colorScheme = Theme.of(context).colorScheme;

    return Padding(
      padding: const EdgeInsets.all(24.0),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,       
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              Text(
                'FIREHORSE TELEMETRY HEARTBEAT',
                style: TextStyle(
                  fontSize: 18, 
                  fontWeight: FontWeight.bold, 
                  letterSpacing: 2,
                  color: colorScheme.onSurface,
                ),
              ),
              _buildTierSelector(context, themeProvider),
            ],
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
              _buildStatCard(context, 'EGRESS STATUS', 'STABLE', colorScheme.primary),
              _buildStatCard(context, 'ACTIVE PIPES', '4', colorScheme.secondary),
              _buildStatCard(context, 'LATENCY', '42ms', colorScheme.tertiary),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildTierSelector(BuildContext context, ThemeProvider themeProvider) {
    return DropdownButton<MaturityTier>(
      value: themeProvider.tier,
      dropdownColor: const Color(0xFF1A1A1A),
      style: TextStyle(color: Theme.of(context).colorScheme.primary),
      underline: Container(
        height: 2,
        color: Theme.of(context).colorScheme.primary,
      ),
      onChanged: (MaturityTier? newValue) {
        if (newValue != null) {
          themeProvider.updateTier(newValue);
        }
      },
      items: MaturityTier.values.map<DropdownMenuItem<MaturityTier>>((MaturityTier value) {
        return DropdownMenuItem<MaturityTier>(
          value: value,
          child: Text(value.name.toUpperCase()),
        );
      }).toList(),
    );
  }

  Widget _buildStatCard(BuildContext context, String label, String value, Color color) {
    final colorScheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: color.withOpacity(0.05),
        border: Border.all(color: color.withOpacity(0.5)),  
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        children: [
          Text(label, style: TextStyle(color: color, fontSize: 10, fontWeight: FontWeight.bold)),
          const SizedBox(height: 8),
          Text(value, style: TextStyle(
            fontSize: 20, 
            fontWeight: FontWeight.bold,
            color: colorScheme.onSurface,
          )),
        ],
      ),
    );
  }
}

class SovereignTreeView extends StatelessWidget {
  const SovereignTreeView({super.key});

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
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
          leading: Icon(Icons.folder_copy_outlined, color: colorScheme.primary),  
          title: Text(repo['name']!, style: TextStyle(color: colorScheme.onSurface)),
          subtitle: Text('Hash: ${repo['hash']}', style: TextStyle(color: colorScheme.onSurfaceVariant)),
          trailing: Chip(
            label: Text(repo['status']!, style: TextStyle(color: _getStatusColor(repo['status']!, colorScheme))),
            backgroundColor: _getStatusColor(repo['status']!, colorScheme).withOpacity(0.1),
            side: BorderSide(color: _getStatusColor(repo['status']!, colorScheme).withOpacity(0.5)),
          ),
        );
      },
    );
  }

  Color _getStatusColor(String status, ColorScheme colorScheme) {
    switch (status) {
      case 'Sovereign':
        return colorScheme.primary;
      case 'Syncing':
        return colorScheme.secondary;
      default:
        return colorScheme.tertiary;
    }
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
    final colorScheme = Theme.of(context).colorScheme;
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, child) {
        return CustomPaint(
          size: const Size(double.infinity, 200),
          painter: SineWavePainter(
            progress: _controller.value,
            color: colorScheme.primary,   
            glowColor: colorScheme.primaryContainer,
          ),
        );
      },
    );
  }
}

class SineWavePainter extends CustomPainter {
  final double progress;
  final Color color;
  final Color glowColor;

  SineWavePainter({required this.progress, required this.color, required this.glowColor});

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = 3.0;

    final path = Path();
    final centerY = size.height / 2;

    for (double x = 0; x <= size.width; x++) {
      final y = centerY + 60 * math.sin((x / size.width * 4 * math.pi) + (progress * 2 * math.pi));
      if (x == 0) {
        path.moveTo(x, y);
      } else {
        path.lineTo(x, y);
      }
    }

    final glowPaint = Paint()
      ..color = glowColor.withOpacity(0.5)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 10.0
      ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 8);
    canvas.drawPath(path, glowPaint);

    canvas.drawPath(path, paint);
  }

  @override
  bool shouldRepaint(covariant SineWavePainter oldDelegate) => true;
}
