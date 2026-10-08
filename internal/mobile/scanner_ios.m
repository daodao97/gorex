//go:build ios && cgo

#import <UIKit/UIKit.h>
#import <AVFoundation/AVFoundation.h>

extern void gorexScanResult(long long token, char *value, char *message);

static UIViewController *gorex_root(void) {
 for (UIScene *scene in UIApplication.sharedApplication.connectedScenes) {
  if (scene.activationState != UISceneActivationStateForegroundActive || ![scene isKindOfClass:UIWindowScene.class]) continue;
  for (UIWindow *window in ((UIWindowScene *)scene).windows) {
   if (!window.isKeyWindow) continue;
   UIViewController *root = window.rootViewController;
   while (root.presentedViewController) root = root.presentedViewController;
   return root;
  }
 }
 return nil;
}

@interface GoRexScanner : UIViewController <AVCaptureMetadataOutputObjectsDelegate>
@property(nonatomic) long long token;
@property(nonatomic) BOOL finished;
@property(nonatomic, strong) AVCaptureSession *capture;
@property(nonatomic, strong) AVCaptureVideoPreviewLayer *preview;
@property(nonatomic, strong) dispatch_queue_t queue;
@end

@implementation GoRexScanner
- (void)viewDidLoad {
 [super viewDidLoad];
 self.view.backgroundColor = UIColor.blackColor;
 self.queue = dispatch_queue_create("dev.gorex.scanner", DISPATCH_QUEUE_SERIAL);
 self.capture = [AVCaptureSession new];
 AVCaptureDevice *device = [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeVideo];
 NSError *error = nil;
 AVCaptureDeviceInput *input = device ? [AVCaptureDeviceInput deviceInputWithDevice:device error:&error] : nil;
 AVCaptureMetadataOutput *output = [AVCaptureMetadataOutput new];
 if (!input || ![self.capture canAddInput:input] || ![self.capture canAddOutput:output]) {
  dispatch_async(dispatch_get_main_queue(), ^{ [self finish:@"" error:@"相机不可用，请粘贴桌面连接码"]; });
  return;
 }
 [self.capture addInput:input];
 [self.capture addOutput:output];
 [output setMetadataObjectsDelegate:self queue:dispatch_get_main_queue()];
 output.metadataObjectTypes = @[AVMetadataObjectTypeQRCode];
 self.preview = [AVCaptureVideoPreviewLayer layerWithSession:self.capture];
 self.preview.videoGravity = AVLayerVideoGravityResizeAspectFill;
 [self.view.layer addSublayer:self.preview];
 UILabel *label = [UILabel new];
 label.text = @"扫描 GoRex 桌面端的二维码";
 label.textColor = UIColor.whiteColor;
 label.textAlignment = NSTextAlignmentCenter;
 label.font = [UIFont preferredFontForTextStyle:UIFontTextStyleHeadline];
 label.translatesAutoresizingMaskIntoConstraints = NO;
 [self.view addSubview:label];
 UIButton *cancel = [UIButton buttonWithType:UIButtonTypeSystem];
 [cancel setTitle:@"取消" forState:UIControlStateNormal];
 [cancel setTitleColor:UIColor.whiteColor forState:UIControlStateNormal];
 cancel.translatesAutoresizingMaskIntoConstraints = NO;
 [cancel addTarget:self action:@selector(cancelScan) forControlEvents:UIControlEventTouchUpInside];
 [self.view addSubview:cancel];
 [NSLayoutConstraint activateConstraints:@[
  [label.topAnchor constraintEqualToAnchor:self.view.safeAreaLayoutGuide.topAnchor constant:60],
  [label.centerXAnchor constraintEqualToAnchor:self.view.centerXAnchor],
  [cancel.bottomAnchor constraintEqualToAnchor:self.view.safeAreaLayoutGuide.bottomAnchor constant:-28],
  [cancel.centerXAnchor constraintEqualToAnchor:self.view.centerXAnchor],
  [cancel.heightAnchor constraintEqualToConstant:48],
  [cancel.widthAnchor constraintEqualToConstant:120]
 ]];
 AVCaptureSession *capture = self.capture;
 dispatch_async(self.queue, ^{ [capture startRunning]; });
}
- (void)viewDidLayoutSubviews {
 [super viewDidLayoutSubviews];
 self.preview.frame = self.view.bounds;
 UIInterfaceOrientation orientation = self.view.window.windowScene.interfaceOrientation;
 if (self.preview.connection.isVideoOrientationSupported) {
  self.preview.connection.videoOrientation = (AVCaptureVideoOrientation)orientation;
 }
}
- (void)cancelScan { [self finish:@"" error:nil]; }
- (void)finish:(NSString *)value error:(NSString *)error {
 if (self.finished) return;
 self.finished = YES;
 AVCaptureSession *capture = self.capture;
 if (capture && self.queue) dispatch_async(self.queue, ^{ [capture stopRunning]; });
 long long token = self.token;
 [self dismissViewControllerAnimated:YES completion:^{
  gorexScanResult(token, (char *)value.UTF8String, error ? (char *)error.UTF8String : NULL);
 }];
}
- (void)captureOutput:(AVCaptureOutput *)output didOutputMetadataObjects:(NSArray<__kindof AVMetadataObject *> *)objects fromConnection:(AVCaptureConnection *)connection {
 for (AVMetadataMachineReadableCodeObject *object in objects) {
  if ([object.type isEqualToString:AVMetadataObjectTypeQRCode] && object.stringValue.length) {
   [self finish:object.stringValue error:nil];
   return;
  }
 }
}
@end

void gorex_scan(long long token) {
 dispatch_async(dispatch_get_main_queue(), ^{
  [AVCaptureDevice requestAccessForMediaType:AVMediaTypeVideo completionHandler:^(BOOL allowed) {
   dispatch_async(dispatch_get_main_queue(), ^{
    if (!allowed) { gorexScanResult(token, "", "请在系统设置中允许 GoRex 使用相机，或粘贴连接码"); return; }
    UIViewController *root = gorex_root();
    if (!root || [root isKindOfClass:GoRexScanner.class]) { gorexScanResult(token, "", "暂时无法打开相机"); return; }
    [root.view endEditing:YES];
    GoRexScanner *scanner = [GoRexScanner new];
    scanner.token = token;
    scanner.modalPresentationStyle = UIModalPresentationFullScreen;
    [root presentViewController:scanner animated:YES completion:nil];
   });
  }];
 });
}

void gorex_hide_keyboard(void) {
 dispatch_async(dispatch_get_main_queue(), ^{
  [UIApplication.sharedApplication sendAction:@selector(resignFirstResponder) to:nil from:nil forEvent:nil];
 });
}

char *gorex_device_name(void) { return strdup(UIDevice.currentDevice.name.UTF8String); }
char *gorex_device_os(void) {
 return strdup([NSString stringWithFormat:@"%@ %@", UIDevice.currentDevice.systemName, UIDevice.currentDevice.systemVersion].UTF8String);
}
