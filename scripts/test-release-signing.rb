# Exercise the workflow shell steps with fixture identities and no keychain access.
require "yaml"
require "tmpdir"
require "fileutils"

steps = YAML.load_file(File.expand_path("../.github/workflows/release.yml", __dir__)).fetch("jobs").values.first.fetch("steps")
setup = steps.find { |s| s["id"] == "signing" }.fetch("run")
verify = steps.find { |s| s["name"].start_with?("Verify code signature") }.fetch("run")
Dir.mktmpdir do |dir|
  Dir.chdir(dir) do
    fingerprint = "A" * 40
    env = {"PATH" => dir + ":/usr/bin:/bin", "RUNNER_TEMP" => dir, "GITHUB_OUTPUT" => File.join(dir, "outputs"),
           "MACOS_CERTIFICATE" => "", "MACOS_SIGNING_IDENTITY" => "", "RELEASE_FINGERPRINT" => fingerprint}
    run = ->(script) { system(env, "/bin/bash", "-eo", "pipefail", "-c", script, out: File::NULL, err: File::NULL) }
    File.write("security", "#!/bin/sh\nexit 99\n")
    File.chmod(0755, "security")
    [["", "fixture"], ["fixture", ""]].each do |cert, identity|
      env.update("MACOS_CERTIFICATE" => cert, "MACOS_SIGNING_IDENTITY" => identity)
      abort "missing signing input passed" if run.call(setup)
    end
    %w[arm64 amd64 universal].each { |arch| FileUtils.mkdir_p("build/darwin-#{arch}/SnipGo.app") }
    File.write("codesign", <<~'SH')
      #!/bin/bash
      if [[ "$1" == "--verify" ]]; then
        [[ "$5" == "-R=identifier \"app.snipgo\" and certificate leaf = H\"$RELEASE_FINGERPRINT\"" && "$VERIFY_RESULT" == ok ]]
      else
        echo "designated => $MOCK_DR"
      fi
    SH
    File.chmod(0755, "codesign")
    env.update("VERIFY_RESULT" => "ok", "MOCK_DR" => 'identifier "app.snipgo" and certificate leaf = H"fixture"')
    abort "valid package rejected" unless run.call(verify)
    env["VERIFY_RESULT"] = "fail"
    abort "invalid signature passed" if run.call(verify)
    env.update("VERIFY_RESULT" => "ok", "MOCK_DR" => "cdhash fixture")
    abort "ad-hoc package passed" if run.call(verify)
  end
end
