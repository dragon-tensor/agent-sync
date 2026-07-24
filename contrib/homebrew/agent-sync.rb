class AgentSync < Formula
  desc "Sync AI chat histories, extract context, build knowledge graphs, and hand off coherent snapshots between tools"
  homepage "https://github.com/agent-sync/agent-sync"
  version "0.1.0"
  license "MIT"

  if OS.mac?
    if Hardware::CPU.arm?
      url "https://github.com/agent-sync/agent-sync/releases/download/v#{version}/agent-sync_v#{version}_macOS_arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000" # placeholder
    else
      url "https://github.com/agent-sync/agent-sync/releases/download/v#{version}/agent-sync_v#{version}_macOS_x86_64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000" # placeholder
    end
  elsif OS.linux?
    if Hardware::CPU.arm?
      url "https://github.com/agent-sync/agent-sync/releases/download/v#{version}/agent-sync_v#{version}_linux_arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000" # placeholder
    else
      url "https://github.com/agent-sync/agent-sync/releases/download/v#{version}/agent-sync_v#{version}_linux_x86_64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000" # placeholder
    end
  end

  def install
    bin.install "agent-sync"
  end

  test do
    assert_match "agent-sync", shell_output("#{bin}/agent-sync --help")
  end
end
