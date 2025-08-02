class Bankshot < Formula
  desc "SSH port forwarding daemon and CLI for remote development workflows"
  homepage "https://github.com/phinze/bankshot"
  url "https://github.com/phinze/bankshot/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "PLACEHOLDER_SHA256"
  license "MIT"
  head "https://github.com/phinze/bankshot.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w"), "./cmd/bankshotd"
    system "go", "build", *std_go_args(ldflags: "-s -w"), "./cmd/bankshot"
  end

  service do
    run [opt_bin/"bankshotd"]
    keep_alive true
    working_dir var
    log_path var/"log/bankshot/bankshot.log"
    error_log_path var/"log/bankshot/bankshot.error.log"
    environment_variables PATH: std_service_path_env
  end

  def post_install
    # Create directories
    (var/"log/bankshot").mkpath
    (var/"run").mkpath

    # Create default config directory
    config_dir = etc/"bankshot"
    config_dir.mkpath

    # Create example config if it doesn't exist
    config_file = config_dir/"config.yaml"
    unless config_file.exist?
      config_file.write <<~EOS
        # Bankshot daemon configuration
        # Network type: unix or tcp
        network: unix

        # Socket address
        # For unix: path to socket file
        # For tcp: host:port
        address: "~/.bankshot.sock"

        # SSH command to use for port forwarding
        ssh_command: ssh

        # Log level: debug, info, warn, error
        log_level: info
      EOS
    end
  end

  def caveats
    <<~EOS
      To start bankshot daemon as a background service:
        brew services start phinze/tap/bankshot

      To configure SSH for bankshot, add to ~/.ssh/config:
        Host *
          ControlMaster auto
          ControlPath /tmp/ssh-%r@%h:%p
          ControlPersist 10m
          RemoteForward ~/.bankshot.sock #{ENV["HOME"]}/.bankshot.sock

      Then on remote servers, you can use:
        bankshot open <url>           # Open URL in local browser
        bankshot forward <port>       # Forward port from remote to local
        bankshot status              # Check daemon status
        bankshot list                # List active forwards

      Or create an alias for compatibility with 'open' command:
        alias open='bankshot open'
    EOS
  end

  test do
    # Test that binaries were built
    assert_predicate bin/"bankshotd", :exist?
    assert_predicate bin/"bankshot", :exist?

    # Test version output
    assert_match "bankshot", shell_output("#{bin}/bankshot --help")
    assert_match "bankshotd", shell_output("#{bin}/bankshotd --help")
  end
end
