import AppKit
import UserNotifications

// MARK: - Argument Parsing

struct Args {
    var title: String = ""
    var body: String = ""
    var url: String = ""
}

func parseArgs() -> Args {
    var args = Args()
    let argv = CommandLine.arguments
    var i = 1
    while i < argv.count {
        switch argv[i] {
        case "--title":
            i += 1
            if i < argv.count { args.title = argv[i] }
        case "--body":
            i += 1
            if i < argv.count { args.body = argv[i] }
        case "--url":
            i += 1
            if i < argv.count { args.url = argv[i] }
        default:
            break
        }
        i += 1
    }
    return args
}

// MARK: - App Delegate

class AppDelegate: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    let args: Args

    init(args: Args) {
        self.args = args
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        let center = UNUserNotificationCenter.current()
        center.delegate = self

        // If launched without a title, we were relaunched by macOS to handle
        // a notification click. Wait for didReceive, but bail if it doesn't
        // come within a few seconds (e.g. notification was dismissed).
        guard !args.title.isEmpty else {
            DispatchQueue.main.asyncAfter(deadline: .now() + 5.0) {
                NSApplication.shared.terminate(nil)
            }
            return
        }

        center.requestAuthorization(options: [.alert, .sound]) { granted, error in
            if let error = error {
                fputs("authorization error: \(error.localizedDescription)\n", stderr)
                NSApplication.shared.terminate(nil)
                return
            }
            guard granted else {
                fputs("notification permission denied\n", stderr)
                NSApplication.shared.terminate(nil)
                return
            }
            self.postNotification(center: center)
        }
    }

    func postNotification(center: UNUserNotificationCenter) {
        let content = UNMutableNotificationContent()
        content.title = args.title
        content.body = args.body
        content.sound = .default

        if !args.url.isEmpty {
            content.userInfo = ["url": args.url]
        }

        let request = UNNotificationRequest(
            identifier: UUID().uuidString,
            content: content,
            trigger: nil
        )

        center.add(request) { error in
            if let error = error {
                fputs("failed to post notification: \(error.localizedDescription)\n", stderr)
            }
            // Give the system a moment to dispatch, then exit
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                NSApplication.shared.terminate(nil)
            }
        }
    }

    // Show banner even when app is in foreground
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .sound])
    }

    // Handle click on notification
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        if let urlString = response.notification.request.content.userInfo["url"] as? String,
           let url = URL(string: urlString) {
            NSWorkspace.shared.open(url)
        }
        completionHandler()
        // Exit after handling the click
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
            NSApplication.shared.terminate(nil)
        }
    }
}

// MARK: - Main

let args = parseArgs()

let app = NSApplication.shared
let delegate = AppDelegate(args: args)
app.delegate = delegate
app.run()
