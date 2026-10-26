#!/usr/bin/env ruby
# Structural regression test for the release DAG. GitHub's default job
# condition requires every prerequisite to succeed; do not replace it with
# always() or a condition that accepts skipped jobs.
require 'yaml'
require 'open3'
require 'tmpdir'
root = File.expand_path('..', __dir__)
release = YAML.load_file(File.join(__dir__, '../.github/workflows/release.yml'))['jobs']
verify = YAML.load_file(File.join(__dir__, '../.github/workflows/verify.yml'))['jobs']
runtime = YAML.load_file(File.join(__dir__, '../.github/workflows/runtime.yml'))['jobs']
required = %w[validate-tag verify source-release binary images images-merge runtime]
raise 'publish must require all artifacts and verification' unless (required - release.fetch('publish').fetch('needs')).empty?
raise 'verification must require tag validation' unless Array(release.fetch('verify')['needs']).include?('validate-tag')
raise 'tag workflow must use the engine version validator' unless release.fetch('validate-tag').fetch('steps').any? { |s| s.fetch('run', '').include?('go run ./cmd/release-version') }
# Older Psych versions cannot parse go-task's colon-bearing flow scalars in
# unrelated tasks (for example [build:web]); inspect the release block alone.
task_block = File.read(File.join(root, 'Taskfile.yml'))[/^  release:tag:\n.*?(?=^  [^ \n]|\z)/m]
task = YAML.load(task_block).fetch('release:tag')
raise 'tag task must use the engine version validator' unless task.fetch('preconditions').any? { |s| s.fetch('sh').include?('go run ./cmd/release-version') }
raise 'runtime publishing must validate its version' unless runtime.fetch('matrix').fetch('steps').any? { |s| s.fetch('run', '').include?('if [ "$push" = true ]; then go run ./cmd/release-version "$RELEASE"') }
%w[binary images runtime].each do |name|
  raise "#{name} can publish before verification" unless Array(release.fetch(name)['needs']).include?('verify')
  raise "#{name} can publish before source preparation" unless Array(release.fetch(name)['needs']).include?('source-release')
end
%w[go install-script web integration compliance].each do |name|
  raise "missing required #{name} verification" unless verify.key?(name)
  raise "#{name} can be conditionally skipped" if verify[name].key?('if') || verify[name]['continue-on-error']
  verify[name].fetch('steps').each do |step|
    raise "#{name} ignores a failed check" if step['continue-on-error']
  end
end
integration_checks = verify.fetch('integration').fetch('steps').map { |step| step['run'] }.compact
raise 'Valkey integration tests are not a release prerequisite' unless integration_checks.any? { |command|
  command.include?('go test -tags integration') && command.include?('./internal/integration') && command.include?('./internal/platform')
}
required.concat(['publish']).each do |name|
  job = release.fetch(name)
  raise "#{name} overrides success prerequisites" if job.key?('if') || job['continue-on-error']
end
# Negative cases: any failed, cancelled, or skipped required check must make
# publication ineligible; the passing case must remain eligible.
eligible = ->(states) { release.fetch('publish').fetch('needs').all? { |id| states[id] == 'success' } }
passing = required.to_h { |id| [id, 'success'] }
raise 'passing release is blocked' unless eligible.call(passing)
required.reject { |id| id == 'publish' }.each do |id|
  %w[failure cancelled skipped].each do |state|
    raise "publication accepts #{id}=#{state}" if eligible.call(passing.merge(id => state))
  end
end
puts 'Release gates: passing case and all failure/cancellation/skip cases verified.'

# Exercise the actual publication shell with a recording gh stub: this cannot
# publish anything. The actual shared validator supplies the classification;
# normal v0 releases must be eligible for latest, just like normal v1 releases.
publish = release.fetch('publish').fetch('steps').find { |step| step['run'] }.fetch('run')
Dir.mktmpdir('presso-release-policy-') do |dir|
  validator = File.join(dir, 'release-version')
  out, err, status = Open3.capture3('go', 'build', '-o', validator, './cmd/release-version', chdir: root)
  raise "cannot build tag validator: #{out}#{err}" unless status.success?
  go_pattern = File.read(File.join(root, 'internal/version/version.go'))[/const ReleasePattern = `([^`]+)`/, 1]
  installer_pattern = File.read(File.join(root, 'install.sh'))[/^release_pattern='([^']+)'$/, 1]
  raise 'installer version policy differs from the engine' unless go_pattern && go_pattern == installer_pattern
  stub = File.join(dir, 'gh')
  File.write(stub, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$PRESSO_TEST_GH_LOG\"\n")
  File.chmod(0755, stub)
  {
    'v0.1.0-alpha.1' => '--prerelease --latest=false',
    'v0.1.0-beta.0' => '--prerelease --latest=false',
    'v0.1.0-rc.1' => '--prerelease --latest=false',
    'v0.1.0' => '--prerelease=false --latest',
    'v0.99.0' => '--prerelease=false --latest',
    'v1.0.0-rc.1' => '--prerelease --latest=false',
    'v1.0.0' => '--prerelease=false --latest',
    'v2.1.0' => '--prerelease=false --latest'
  }.each do |tag, flags|
    classification, err, status = Open3.capture3(validator, tag)
    raise "valid tag rejected: #{tag}: #{err}" unless status.success?
    _, _, status = Open3.capture3({'TAG' => tag, 'PATTERN' => installer_pattern}, 'bash', '-c', '[[ "$TAG" =~ $PATTERN ]]')
    raise "installer rejects valid tag #{tag}" unless status.success?
    log = File.join(dir, 'calls')
    File.write(log, '')
    env = {
      'PATH' => "#{dir}:#{ENV.fetch('PATH')}",
      'TAG' => tag,
      'PRERELEASE' => classification.strip,
      'GITHUB_REPOSITORY' => 'test/presso',
      'GH_TOKEN' => 'not-a-real-token',
      'PRESSO_TEST_GH_LOG' => log
    }
    out, err, status = Open3.capture3(env, 'bash', '-c', publish)
    raise "publish script failed for #{tag}: #{out}#{err}" unless status.success?
    expected = "release edit #{tag} --repo test/presso --draft=false #{flags}"
    raise "wrong publication flags for #{tag}: #{File.read(log)}" unless File.readlines(log).first&.strip == expected
  end
  ['', '0.1.0', 'v0.1', 'v0.1.0-rc1', 'v0.1.0-alpha', 'v0.1.0-preview.1',
   'v00.1.0', 'v0.01.0', 'v0.1.00', 'v0.1.0-rc.01', 'v0.1.0+build.1',
   'v0.1.0-3-gabc1234', 'v0.0.0-dev', "v0.1.0\n"].each do |tag|
    _, _, status = Open3.capture3(validator, tag)
    raise "invalid tag accepted: #{tag.inspect}" if status.success?
    _, _, status = Open3.capture3({'TAG' => tag, 'PATTERN' => installer_pattern}, 'bash', '-c', '[[ "$TAG" =~ $PATTERN ]]')
    raise "installer accepts invalid tag: #{tag.inspect}" if status.success?
  end
  log = File.join(dir, 'calls')
  File.write(log, '')
  env = {'PATH' => "#{dir}:#{ENV.fetch('PATH')}", 'PRERELEASE' => '', 'PRESSO_TEST_GH_LOG' => log}
  _, _, status = Open3.capture3(env, 'bash', '-c', publish)
  raise 'missing classification can publish' if status.success? || !File.read(log).empty?
end
puts 'Release policy: canonical dotted tags enforced; plain v0+ releases are latest, alpha/beta/rc are prereleases.'

# Docker builds use separate contexts, so their license/notice copies must
# remain identical to the root originals.
%w[LICENSE NOTICE].each do |name|
  %w[build runtime/mu-plugins].each do |dir|
    raise "#{dir}/#{name} differs from root #{name}" unless File.binread(File.join(root, dir, name)) == File.binread(File.join(root, name))
  end
end
puts 'Distribution license and notice copies match.'

# Every image build loads locally. Only the source-verifying wrapper may push
# new payloads; merge jobs may publish indexes of those already checked images.
[[release, 'images'], [runtime, 'base'], [runtime, 'image']].each do |jobs, name|
  steps = jobs.fetch(name).fetch('steps')
  builds = steps.select { |step| step.fetch('uses', '').start_with?('docker/build-push-action@') }
  raise "#{name} has no local image build" if builds.empty?
  builds.each do |step|
    config = step.fetch('with')
    raise "#{name} bypasses source-before-image publication" unless config['load'] == true && !config['push'] && !config.fetch('outputs', '').include?('push=true')
  end
  commands = steps.map { |step| step['run'] }.compact
  raise "#{name} omits verified source publication" unless commands.any? { |cmd| cmd.include?('scripts/compliance/release.py image') && cmd.include?('--push') }
  raise "#{name} directly pushes an unchecked image" if commands.any? { |cmd| cmd.match?(/docker\s+(push|buildx\s+build)/) }
end
# Every platform image the engine pulls is built, source-verified and merged.
platform = Dir.glob(File.join(root, 'build/*.Dockerfile')).map { |f| File.basename(f, '.Dockerfile') } - ['pressod']
%w[images images-merge].each do |name|
  built = release.fetch(name).fetch('strategy').fetch('matrix').fetch('image')
  raise "#{name} does not cover every platform image: #{(platform - built).join(', ')}" unless (platform - built).empty? && (built - platform).empty?
end
raise 'base publication does not require public sources' unless Array(runtime.fetch('base')['needs']).include?('source-release')
raise 'runtime verification bypasses final indexes' unless Array(runtime.fetch('verify')['needs']).include?('image-merge')
config = YAML.load_file(File.join(root, '.goreleaser.yaml'))
raise 'binary source verification hook missing' unless config.fetch('builds').all? { |b| b.fetch('hooks').fetch('post').any? { |h| h.fetch('cmd').include?('scripts/compliance/release.py binary') } }
raise 'binary sources will not be published' unless release.fetch('binary').fetch('steps').any? { |s| s.fetch('env', {})['PRESSO_PUBLISH_SOURCES'] == '1' }
puts 'Sources and notices: all image pushes and binary archives require verified materials.'
